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

// ComputeIPPoolStats derives the capacity statistics for an IP address pool from a
// single snapshot of its spec and the IPAddress objects currently belonging to it.
//
// The allocatable set (total), the out-of-range set and the excluded count are all
// derived from the same poolIPSet, so the resulting numbers can never contradict
// each other. The function is free of mutable shared state and safe to call
// concurrently with the same inputs.
func ComputeIPPoolStats(spec *v1alpha2.InClusterIPPoolSpec, inUse []ipamv1.IPAddress) (Stats, error) {
	// The allocatable set: spec.addresses merged, with exclusions, reserved
	// addresses and the gateway already removed (see poolutil.PoolSpecToIPSet).
	poolIPSet, err := poolutil.PoolSpecToIPSet(spec)
	if err != nil {
		return Stats{}, err
	}
	totalBig := poolutil.IPSetCountBig(poolIPSet)

	// The raw set declared by spec.addresses, before any removal. The set
	// difference raw \ allocatable is the excluded count; doing the arithmetic on
	// merged IPSets means overlapping removals are counted only once.
	rawIPSet, err := poolutil.AddressesToIPSet(spec.Addresses)
	if err != nil {
		return Stats{}, err
	}
	excludedBig := new(big.Int).Sub(poolutil.IPSetCountBig(rawIPSet), totalBig)

	// Out-of-range allocations are computed against the very same poolIPSet used
	// for the total. Unparsable addresses are skipped by AddressesOutOfRangeIPSet
	// while still being counted as in-use objects below.
	outOfRangeIPSet, err := poolutil.AddressesOutOfRangeIPSet(inUse, poolIPSet)
	if err != nil {
		return Stats{}, err
	}
	outOfRangeBig := poolutil.IPSetCountBig(outOfRangeIPSet)

	total, totalCapped := poolutil.CapBigInt(totalBig)
	excluded, excludedCapped := poolutil.CapBigInt(excludedBig)
	outOfRange, outOfRangeCapped := poolutil.CapBigInt(outOfRangeBig)

	// Used counts the actual IPAddress objects, not the set contents, so every
	// object contributes exactly one and its value cannot exceed the int range.
	used := len(inUse)

	// Free is computed from the capped status numbers and clamped at zero. This
	// guarantees a non-negative free even when out-of-range allocations make used
	// larger than total.
	free := max(0, total-used)

	return Stats{
		Total:                 total,
		Used:                  used,
		Free:                  free,
		OutOfRange:            outOfRange,
		Excluded:              excluded,
		Exhausted:             free == 0,
		CountsCapped:          totalCapped || excludedCapped || outOfRangeCapped,
		OutOfRangeAllocations: outOfRange > 0,
	}, nil
}
