/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InClusterPrefixPoolSpec defines the desired state of InClusterPrefixPool.
type InClusterPrefixPoolSpec struct {
	// Prefixes is a list of IPv6 CIDRs from which sub-prefixes are allocated.
	// Each entry must be a network-aligned CIDR (e.g. "fd00::/48").
	Prefixes []string `json:"prefixes"`

	// AllocationPrefixLength is the prefix length of each allocated
	// sub-prefix (e.g. 64 means /64 allocations). Defaults to 64.
	// Must be >= the prefix length of each entry in Prefixes and <= 128.
	// +kubebuilder:default:=64
	// +kubebuilder:validation:Maximum=128
	// +kubebuilder:validation:Minimum=1
	// +optional
	AllocationPrefixLength int `json:"allocationPrefixLength,omitempty"`

	// Gateway is the next-hop IPv6 address carried unchanged to every
	// allocated IPAddress.Spec.Gateway. Unlike IP pools, the gateway does
	// NOT block any prefix from being allocated — it is informational only.
	// +optional
	Gateway string `json:"gateway,omitempty"`

	// ExcludedPrefixes lists IPs/CIDRs/ranges to exclude from allocation.
	// Any candidate sub-prefix overlapping an excluded entry is skipped.
	// +optional
	ExcludedPrefixes []string `json:"excludedPrefixes,omitempty"`
}

// InClusterPrefixPoolStatus defines the observed state of InClusterPrefixPool.
type InClusterPrefixPoolStatus struct {
	// Addresses reports the count of total, free, and used prefixes in the pool.
	// The JSON tag is "ipAddresses" to match the InClusterIPPool status shape,
	// keeping a consistent status contract across pool types.
	// +optional
	Addresses *InClusterPrefixPoolStatusAddresses `json:"ipAddresses,omitempty"`
}

// InClusterPrefixPoolStatusAddresses contains the count of total, free, and used prefixes in a pool.
//
// The accounting mirrors InClusterIPPoolStatusIPAddresses: used counts every
// IPAddress object (including out-of-range ones), free is clamped at zero, and the
// counts are advisory only. Allocation reads live objects rather than this status.
type InClusterPrefixPoolStatusAddresses struct {
	// Total is the total number of allocatable sub-prefixes in the pool.
	// Counts greater than int can contain will report as math.MaxInt.
	Total int `json:"total"`

	// Free is the count of unallocated sub-prefixes. It is clamped at zero and
	// therefore never negative, even when allocations exist that fall outside the
	// current allocatable set (see outOfRange).
	// Counts greater than int can contain will report as math.MaxInt.
	Free int `json:"free"`

	// Used is the count of allocated sub-prefixes. It counts every IPAddress object
	// that belongs to the pool, including out-of-range ones, i.e. used == in-range
	// allocations + outOfRange.
	// Counts greater than int can contain will report as math.MaxInt.
	Used int `json:"used"`

	// OutOfRange is the count of allocated prefixes currently non-allocatable
	// under spec (outside spec.Prefixes, overlapped by spec.ExcludedPrefixes,
	// or with the wrong prefix length).
	// Counts greater than int can contain will report as math.MaxInt.
	OutOfRange int `json:"outOfRange"`

	// Excluded is the count of candidate sub-prefixes removed from the pool by
	// spec.ExcludedPrefixes. The informational gateway does not block prefix
	// allocation and therefore never contributes to this count.
	// Counts greater than int can contain will report as math.MaxInt.
	// +optional
	Excluded int `json:"excluded,omitempty"`

	// Exhausted is true when Free is zero. It is the stable signal for upper-layer
	// automation to alert on or trigger pool expansion.
	// +optional
	Exhausted bool `json:"exhausted,omitempty"`

	// CountsCapped is true when at least one of the integer counts is larger than
	// the platform int range and therefore reported as math.MaxInt instead of its
	// exact value. It only describes the precision of the numbers; it never
	// participates in the Free or Exhausted calculation.
	// +optional
	CountsCapped bool `json:"countsCapped,omitempty"`

	// OutOfRangeAllocations is true when OutOfRange is greater than zero. It
	// highlights the anomalous state where existing allocations are no longer
	// allocatable under the current spec.
	// +optional
	OutOfRangeAllocations bool `json:"outOfRangeAllocations,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=cluster-api
// +kubebuilder:printcolumn:name="Prefixes",type="string",JSONPath=".spec.prefixes",description="List of prefixes to allocate sub-prefixes from"
// +kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.ipAddresses.total",description="Count of allocatable sub-prefixes in the pool"
// +kubebuilder:printcolumn:name="Free",type="integer",JSONPath=".status.ipAddresses.free",description="Count of unallocated sub-prefixes in the pool"
// +kubebuilder:printcolumn:name="Used",type="integer",JSONPath=".status.ipAddresses.used",description="Count of allocated sub-prefixes in the pool"
// +kubebuilder:printcolumn:name="Exhausted",type="boolean",JSONPath=".status.ipAddresses.exhausted",description="True when no free sub-prefixes remain in the pool"

// InClusterPrefixPool is the Schema for the inclusterprefixpools API.
type InClusterPrefixPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InClusterPrefixPoolSpec   `json:"spec,omitempty"`
	Status InClusterPrefixPoolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// InClusterPrefixPoolList contains a list of InClusterPrefixPool.
type InClusterPrefixPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InClusterPrefixPool `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster,categories=cluster-api
// +kubebuilder:printcolumn:name="Prefixes",type="string",JSONPath=".spec.prefixes",description="List of prefixes to allocate sub-prefixes from"
// +kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.ipAddresses.total",description="Count of allocatable sub-prefixes in the pool"
// +kubebuilder:printcolumn:name="Free",type="integer",JSONPath=".status.ipAddresses.free",description="Count of unallocated sub-prefixes in the pool"
// +kubebuilder:printcolumn:name="Used",type="integer",JSONPath=".status.ipAddresses.used",description="Count of allocated sub-prefixes in the pool"
// +kubebuilder:printcolumn:name="Exhausted",type="boolean",JSONPath=".status.ipAddresses.exhausted",description="True when no free sub-prefixes remain in the pool"

// GlobalInClusterPrefixPool is the Schema for the global inclusterprefixpools API.
// This pool type is cluster scoped. IPAddressClaims can reference
// pools of this type from any namespace.
type GlobalInClusterPrefixPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InClusterPrefixPoolSpec   `json:"spec,omitempty"`
	Status InClusterPrefixPoolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GlobalInClusterPrefixPoolList contains a list of GlobalInClusterPrefixPool.
type GlobalInClusterPrefixPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlobalInClusterPrefixPool `json:"items"`
}

func init() {
	objectTypes = append(objectTypes,
		&InClusterPrefixPool{},
		&InClusterPrefixPoolList{},
		&GlobalInClusterPrefixPool{},
		&GlobalInClusterPrefixPoolList{},
	)
}

// PoolSpec implements the genericInClusterPrefixPool interface.
func (p *InClusterPrefixPool) PoolSpec() *InClusterPrefixPoolSpec {
	return &p.Spec
}

// PoolStatus implements the genericInClusterPrefixPool interface.
func (p *InClusterPrefixPool) PoolStatus() *InClusterPrefixPoolStatus {
	return &p.Status
}

// PoolSpec implements the genericInClusterPrefixPool interface.
func (p *GlobalInClusterPrefixPool) PoolSpec() *InClusterPrefixPoolSpec {
	return &p.Spec
}

// PoolStatus implements the genericInClusterPrefixPool interface.
func (p *GlobalInClusterPrefixPool) PoolStatus() *InClusterPrefixPoolStatus {
	return &p.Status
}
