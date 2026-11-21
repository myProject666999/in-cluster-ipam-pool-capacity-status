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

package poolstats_test

import (
	"math"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"

	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/api/v1alpha2"
	"sigs.k8s.io/cluster-api-ipam-provider-in-cluster/internal/poolstats"
)

func metaName(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "default"}
}

func inUseIP(name, address string) ipamv1.IPAddress {
	return ipamv1.IPAddress{
		ObjectMeta: metaName(name),
		Spec: ipamv1.IPAddressSpec{
			Address: address,
			Prefix:  ptr.To(int32(24)), //nolint:gosec // test fixture
		},
	}
}

func inUsePrefix(name, address string, prefixLen int32) ipamv1.IPAddress {
	return ipamv1.IPAddress{
		ObjectMeta: metaName(name),
		Spec: ipamv1.IPAddressSpec{
			Address: address,
			Prefix:  ptr.To(prefixLen),
		},
	}
}

var _ = Describe("ComputeIPPoolStats", func() {
	It("reports a small pool filling up and being released again", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses: []string{"10.0.0.10-10.0.0.12"},
			Prefix:    24,
		}

		empty, err := poolstats.ComputeIPPoolStats(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(empty).To(Equal(poolstats.Stats{Total: 3, Used: 0, Free: 3, Exhausted: false}))
		Expect(empty.EqualsIPStatus(nil)).To(BeFalse(), "a nil status must always count as a change")

		full, err := poolstats.ComputeIPPoolStats(spec, []ipamv1.IPAddress{
			inUseIP("a", "10.0.0.10"),
			inUseIP("b", "10.0.0.11"),
			inUseIP("c", "10.0.0.12"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(full).To(Equal(poolstats.Stats{Total: 3, Used: 3, Free: 0, Exhausted: true}))

		released, err := poolstats.ComputeIPPoolStats(spec, []ipamv1.IPAddress{
			inUseIP("a", "10.0.0.10"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(released).To(Equal(poolstats.Stats{Total: 3, Used: 1, Free: 2, Exhausted: false}))
		Expect(released.EqualsIPStatus(full.ToIPStatus())).To(BeFalse())
		Expect(released.EqualsIPStatus(released.ToIPStatus())).To(BeTrue(), "round-tripped status must be equal")
	})

	It("clamps free at zero when a pool shrink leaves used above total", func() {
		// After shrinking the range to two addresses, three allocations remain, two
		// of which now fall outside the allocatable set.
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"10.0.0.10-10.0.0.11"},
			Prefix:                      24,
			AllocateReservedIPAddresses: true,
		}
		stats, err := poolstats.ComputeIPPoolStats(spec, []ipamv1.IPAddress{
			inUseIP("a", "10.0.0.10"),
			inUseIP("b", "10.0.0.20"),
			inUseIP("c", "10.0.0.21"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Total).To(Equal(2))
		Expect(stats.Used).To(Equal(3))
		Expect(stats.Free).To(Equal(0), "free must never be negative")
		Expect(stats.OutOfRange).To(Equal(2))
		Expect(stats.OutOfRangeAllocations).To(BeTrue())
		Expect(stats.Exhausted).To(BeTrue())
	})

	It("counts excluded addresses and flags exclusions covering existing allocations", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"10.0.0.10-10.0.0.20"},
			ExcludedAddresses:           []string{"10.0.0.15-10.0.0.17"},
			Prefix:                      24,
			AllocateReservedIPAddresses: true,
		}
		stats, err := poolstats.ComputeIPPoolStats(spec, []ipamv1.IPAddress{
			inUseIP("a", "10.0.0.15"), // now covered by the new exclusion
			inUseIP("b", "10.0.0.18"),
		})
		Expect(err).NotTo(HaveOccurred())
		// 11 raw addresses, 3 excluded => 8 allocatable.
		Expect(stats.Excluded).To(Equal(3))
		Expect(stats.Total).To(Equal(8))
		Expect(stats.Used).To(Equal(2))
		Expect(stats.OutOfRange).To(Equal(1))
		Expect(stats.OutOfRangeAllocations).To(BeTrue())
		Expect(stats.Free).To(Equal(6))
	})

	It("counts the gateway and reserved addresses as excluded without double counting", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses: []string{"10.0.0.0/24"},
			Prefix:    24,
			Gateway:   "10.0.0.1",
		}
		stats, err := poolstats.ComputeIPPoolStats(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		// Raw /24 has 256 addresses; network, broadcast and gateway are removed, and
		// the invariant total + excluded == raw count must hold.
		Expect(stats.Excluded).To(Equal(3))
		Expect(stats.Total).To(Equal(253))
		Expect(stats.Total + stats.Excluded).To(Equal(256))
	})

	It("counts exclusions outside the raw address set as zero", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"10.0.0.10-10.0.0.20"},
			ExcludedAddresses:           []string{"10.0.9.1"},
			Prefix:                      24,
			AllocateReservedIPAddresses: true,
		}
		stats, err := poolstats.ComputeIPPoolStats(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Excluded).To(Equal(0))
		Expect(stats.Total).To(Equal(11))
	})

	It("caps an IPv6 /64 pool at MaxInt without reporting exhaustion", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"fd00::/64"},
			Prefix:                      64,
			AllocateReservedIPAddresses: true,
		}
		stats, err := poolstats.ComputeIPPoolStats(spec, []ipamv1.IPAddress{
			inUseIP("a", "fd00::1"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Total).To(Equal(math.MaxInt))
		Expect(stats.CountsCapped).To(BeTrue())
		Expect(stats.Used).To(Equal(1))
		// The capped total is treated as the displayed capacity, so free still
		// decreases by exactly one and never overflows back to a wrong value.
		Expect(stats.Free).To(Equal(math.MaxInt - 1))
		Expect(stats.Exhausted).To(BeFalse(), "a capped pool with tiny usage must not look exhausted")
	})

	It("still counts unparsable in-use addresses while excluding them from out-of-range set logic", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"10.0.0.10-10.0.0.12"},
			Prefix:                      24,
			AllocateReservedIPAddresses: true,
		}
		stats, err := poolstats.ComputeIPPoolStats(spec, []ipamv1.IPAddress{
			inUseIP("a", "not-an-ip"),
			inUseIP("b", "10.0.0.10"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Used).To(Equal(2), "every object counts as used, even an unparsable one")
		Expect(stats.OutOfRange).To(Equal(0), "the unparsable address is skipped by the out-of-range set builder")
		Expect(stats.Free).To(Equal(1))
	})

	It("is deterministic under concurrent calls over a shared snapshot", func() {
		spec := &v1alpha2.InClusterIPPoolSpec{
			Addresses:                   []string{"10.0.0.10-10.0.0.30"},
			ExcludedAddresses:           []string{"10.0.0.20"},
			Prefix:                      24,
			AllocateReservedIPAddresses: true,
		}
		inUse := []ipamv1.IPAddress{
			inUseIP("a", "10.0.0.10"),
			inUseIP("b", "10.0.0.11"),
			inUseIP("c", "10.0.0.40"), // out of range
		}

		expected, err := poolstats.ComputeIPPoolStats(spec, inUse)
		Expect(err).NotTo(HaveOccurred())

		const goroutines = 32
		var wg sync.WaitGroup
		results := make([]poolstats.Stats, goroutines)
		errs := make([]error, goroutines)
		wg.Add(goroutines)
		for i := range goroutines {
			go func(i int) {
				defer wg.Done()
				results[i], errs[i] = poolstats.ComputeIPPoolStats(spec, inUse)
			}(i)
		}
		wg.Wait()

		for i := range goroutines {
			Expect(errs[i]).NotTo(HaveOccurred())
			Expect(results[i]).To(Equal(expected))
		}
	})
})

var _ = Describe("ComputePrefixPoolStats", func() {
	It("reports allocatable /64 prefixes and usage in a small pool", func() {
		spec := &v1alpha2.InClusterPrefixPoolSpec{
			Prefixes: []string{"fd00::/62"},
		}
		stats, err := poolstats.ComputePrefixPoolStats(spec, []ipamv1.IPAddress{
			inUsePrefix("a", "fd00::", 64),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(stats).To(Equal(poolstats.Stats{Total: 4, Used: 1, Free: 3, Exhausted: false}))
	})

	It("reports out-of-range allocations after a shrink and clamps free", func() {
		spec := &v1alpha2.InClusterPrefixPoolSpec{
			Prefixes: []string{"fd00:0:0:1::/64"},
		}
		stats, err := poolstats.ComputePrefixPoolStats(spec, []ipamv1.IPAddress{
			inUsePrefix("a", "fd00::", 64), // no longer inside the shrunk pool
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Total).To(Equal(1))
		Expect(stats.Used).To(Equal(1))
		Expect(stats.OutOfRange).To(Equal(1))
		Expect(stats.OutOfRangeAllocations).To(BeTrue())
		Expect(stats.Free).To(Equal(0))
		Expect(stats.Exhausted).To(BeTrue())
	})

	It("counts prefixes removed by excludedPrefixes while the gateway contributes nothing", func() {
		spec := &v1alpha2.InClusterPrefixPoolSpec{
			Prefixes:         []string{"fd00::/62"},
			ExcludedPrefixes: []string{"fd00:0:0:1::/64"},
			Gateway:          "fd00::1",
		}
		stats, err := poolstats.ComputePrefixPoolStats(spec, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Excluded).To(Equal(1))
		Expect(stats.Total).To(Equal(3))
	})

	It("caps a large IPv6 prefix pool at MaxInt", func() {
		// A /0 pool allocating /64 prefixes has 2^64 candidates.
		spec := &v1alpha2.InClusterPrefixPoolSpec{
			Prefixes:               []string{"::/0"},
			AllocationPrefixLength: 64,
		}
		stats, err := poolstats.ComputePrefixPoolStats(spec, []ipamv1.IPAddress{
			inUsePrefix("a", "::", 64),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(stats.Total).To(Equal(math.MaxInt))
		Expect(stats.CountsCapped).To(BeTrue())
		Expect(stats.Used).To(Equal(1))
		Expect(stats.Free).To(Equal(math.MaxInt - 1))
		Expect(stats.Exhausted).To(BeFalse())
	})
})
