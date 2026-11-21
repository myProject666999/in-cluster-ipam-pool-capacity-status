/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Package poolstats computes the capacity statistics reported in pool status objects.

It deliberately has no dependency on controller-runtime clients: callers hand in a
snapshot consisting of the pool spec and the IPAddress objects read at one point in
time, and the functions in this package derive all counts from that single snapshot.
This keeps total, used and free mutually consistent and makes the architectural
boundary explicit — status statistics are advisory output for users and external
automation, while allocation decisions must keep reading the live address objects.
*/
package poolstats

import (
	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/api/v1alpha2"
)

// Stats is the kind-agnostic, in-memory representation of the numbers reported in a
// pool status. Every integer field is capped at math.MaxInt; CountsCapped records
// when that happened.
type Stats struct {
	// Total is the size of the allocatable set derived from the (merged) pool spec.
	Total int
	// Used is the number of IPAddress objects belonging to the pool, including
	// out-of-range ones.
	Used int
	// Free is Total minus Used, clamped at zero.
	Free int
	// OutOfRange is the number of allocations that are not part of the allocatable set.
	OutOfRange int
	// Excluded is the number of candidates removed from the raw spec set by
	// exclusions, reserved addresses or the gateway (IP pools only).
	Excluded int

	// Exhausted is true exactly when Free is zero.
	Exhausted bool
	// CountsCapped is true when at least one count exceeded math.MaxInt.
	CountsCapped bool
	// OutOfRangeAllocations is true exactly when OutOfRange is greater than zero.
	OutOfRangeAllocations bool
}

// Equals reports whether two Stats values carry identical content.
func (s Stats) Equals(other Stats) bool {
	return s == other
}

// EqualsIPStatus reports whether the given IP pool status pointer carries the same
// statistics. A nil status (the state before the first reconciliation) is only equal
// to a nil-valued Stats, so the first population always counts as a change.
func (s Stats) EqualsIPStatus(status *v1alpha2.InClusterIPPoolStatusIPAddresses) bool {
	if status == nil {
		return false
	}
	return s == Stats{
		Total:                 status.Total,
		Used:                  status.Used,
		Free:                  status.Free,
		OutOfRange:            status.OutOfRange,
		Excluded:              status.Excluded,
		Exhausted:             status.Exhausted,
		CountsCapped:          status.CountsCapped,
		OutOfRangeAllocations: status.OutOfRangeAllocations,
	}
}

// EqualsPrefixStatus reports whether the given prefix pool status pointer carries the
// same statistics. A nil status is never equal.
func (s Stats) EqualsPrefixStatus(status *v1alpha2.InClusterPrefixPoolStatusAddresses) bool {
	if status == nil {
		return false
	}
	return s == Stats{
		Total:                 status.Total,
		Used:                  status.Used,
		Free:                  status.Free,
		OutOfRange:            status.OutOfRange,
		Excluded:              status.Excluded,
		Exhausted:             status.Exhausted,
		CountsCapped:          status.CountsCapped,
		OutOfRangeAllocations: status.OutOfRangeAllocations,
	}
}

// ToIPStatus converts the Stats into the v1alpha2 IP pool status representation.
func (s Stats) ToIPStatus() *v1alpha2.InClusterIPPoolStatusIPAddresses {
	return &v1alpha2.InClusterIPPoolStatusIPAddresses{
		Total:                 s.Total,
		Free:                  s.Free,
		Used:                  s.Used,
		OutOfRange:            s.OutOfRange,
		Excluded:              s.Excluded,
		Exhausted:             s.Exhausted,
		CountsCapped:          s.CountsCapped,
		OutOfRangeAllocations: s.OutOfRangeAllocations,
	}
}

// ToPrefixStatus converts the Stats into the v1alpha2 prefix pool status representation.
func (s Stats) ToPrefixStatus() *v1alpha2.InClusterPrefixPoolStatusAddresses {
	return &v1alpha2.InClusterPrefixPoolStatusAddresses{
		Total:                 s.Total,
		Free:                  s.Free,
		Used:                  s.Used,
		OutOfRange:            s.OutOfRange,
		Excluded:              s.Excluded,
		Exhausted:             s.Exhausted,
		CountsCapped:          s.CountsCapped,
		OutOfRangeAllocations: s.OutOfRangeAllocations,
	}
}
