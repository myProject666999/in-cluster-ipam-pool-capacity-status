/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package poolstats

import (
	"math/big"

	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"

	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/api/v1alpha2"
	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/internal/poolutil"
)

// ComputePrefixPoolStats derives the capacity statistics for a prefix pool from a
// single snapshot of its spec and the IPAddress objects (each carrying one allocated
// sub-prefix) currently belonging to it. It shares the accounting rules of
// ComputeIPPoolStats so both pool kinds expose the same status contract.
func ComputePrefixPoolStats(spec *v1alpha2.InClusterPrefixPoolSpec, inUse []ipamv1.IPAddress) (Stats, error) {
	config := poolutil.NewPrefixPoolConfig(spec)

	totalBig, err := poolutil.PrefixCandidateCountBig(config)
	if err != nil {
		return Stats{}, err
	}

	// Excluded candidates are the difference between the pool's full candidate set
	// and the candidate set left after excludedPrefixes are applied. The gateway is
	// intentionally absent from this computation because it never blocks prefix
	// allocation (see poolutil.prefixPoolBlockedIPSet).
	excludedBig := big.NewInt(0)
	if len(config.ExcludedPrefixes) > 0 {
		withoutExclusions := *config
		withoutExclusions.ExcludedPrefixes = nil
		rawCandidates, err := poolutil.PrefixCandidateCountBig(&withoutExclusions)
		if err != nil {
			return Stats{}, err
		}
		excludedBig = new(big.Int).Sub(rawCandidates, totalBig)
	}

	outOfRange := 0
	for _, inUseAddress := range inUse {
		prefix, err := poolutil.PrefixFromIPAddress(inUseAddress)
		if err != nil || !poolutil.PrefixIsAllocatable(prefix, config) {
			outOfRange++
		}
	}

	total, totalCapped := poolutil.CapBigInt(totalBig)
	excluded, excludedCapped := poolutil.CapBigInt(excludedBig)

	// Used counts actual allocation objects and cannot exceed the int range.
	used := len(inUse)

	// Same clamping rule as IP address pools: out-of-range allocations stay counted
	// in used while free is never negative.
	free := max(0, total-used)

	return Stats{
		Total:                 total,
		Used:                  used,
		Free:                  free,
		OutOfRange:            outOfRange,
		Excluded:              excluded,
		Exhausted:             free == 0,
		CountsCapped:          totalCapped || excludedCapped,
		OutOfRangeAllocations: outOfRange > 0,
	}, nil
}
