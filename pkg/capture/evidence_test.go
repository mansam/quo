package capture

import (
	"fmt"
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/kvo/pkg/bundle"
	"github.com/mansam/kvo/pkg/cluster"
)

// The storage the fixture VirtualMachine's claims were provisioned from.
const (
	testStorageClass = "ocs-storagecluster-ceph-rbd"
	testProvisioner  = "openshift-storage.rbd.csi.ceph.com"
	testBindingMode  = "WaitForFirstConsumer"
	testVolumeMode   = "Block"
	testCapacity     = "32Gi"
	testRootPV       = "pvc-9e1f0f3c-4a2e-4c2a-9bf0-1f6a1e2d3c4b"
	testDataPV       = "pvc-2b84c7a1-55d3-4e18-ae29-7c0b5f4a8d61"
)

func TestExportCapturesTheStorageBehindEveryPersistentVolumeClaim(t *testing.T) {
	tests := []struct {
		name string
		kind string
		// object is the cluster-scoped name the claim led quo to.
		object string
		// held is what the evidence has to carry: the reason the object is
		// worth capturing at all.
		held string
	}{
		{
			name:   "the StorageClass a claim was provisioned from, with its provisioner and binding mode",
			kind:   cluster.KindStorageClass,
			object: testStorageClass,
			held:   testBindingMode,
		},
		{
			name:   "the CDI StorageProfile that decided the volume mode the disk got",
			kind:   cluster.KindStorageProfile,
			object: testStorageClass,
			held:   testVolumeMode,
		},
		{
			name:   "the PersistentVolume a claim is bound to, with its real capacity",
			kind:   cluster.KindPersistentVolume,
			object: testRootPV,
			held:   testCapacity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			_, content := captureWithDisks(t, provisionedCluster(t), testOptions())

			member := bundle.EvidencePath(tt.kind, tt.object)
			g.Expect(content).To(gomega.HaveKey(member))
			object := decodeMember(t, content, member)
			g.Expect(object["kind"]).To(gomega.Equal(tt.kind))
			// The platform configuration is frequently where the problem
			// actually is, so it travels rather than being summarised.
			g.Expect(string(content[member])).To(gomega.ContainSubstring(tt.held))
			manifest := capturedManifest(t, content)
			// It is evidence about the source cluster, not something a target
			// would be given.
			g.Expect(gathered(manifest)).To(gomega.ContainElement(clusterRef(tt.kind, tt.object)))
			g.Expect(carried(manifest)).ToNot(gomega.ContainElement(clusterRef(tt.kind, tt.object)))
		})
	}
}

func TestExportCapturesKubeVirtsOwnConfigurationIncludingItsFeatureGates(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureWithDisks(t, stoppedCluster(t), testOptions())

	kv := decodeMember(t, content, testKubeVirtMember)
	g.Expect(kv["kind"]).To(gomega.Equal(cluster.KindKubeVirt))
	// The enabled feature gates are what account for behaviour a target
	// cluster does not reproduce.
	developer := nested(t, kv, "spec", "configuration", "developerConfiguration")
	g.Expect(developer["featureGates"]).To(gomega.ConsistOf(testFeatureGate))
	manifest := capturedManifest(t, content)
	g.Expect(gathered(manifest)).To(gomega.ContainElement(
		refIn(cluster.KindKubeVirt, testKubeVirtNamespace, testKubeVirtName)))
}

func TestExportCapturesTheExpandedFormWithoutUsingItToRewriteTheVirtualMachine(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureWithDisks(t, stoppedCluster(t), testOptions())

	// The expanded form carries the machine type and CPU topology the source
	// cluster resolved, neither of which a stopped VirtualMachine has.
	expanded := nested(t, decodeMember(t, content, testExpandedMember),
		"spec", "template", "spec", "domain")
	g.Expect(nested(t, expanded, "machine")["type"]).To(gomega.Equal(testMachineType))
	g.Expect(nested(t, expanded, "cpu")["cores"]).To(gomega.BeEquivalentTo(testExpandedCores))
	// The VirtualMachine the bundle carries is the one the cluster holds,
	// unexpanded. Expansion is version-dependent, and a difference between
	// what the source cluster resolved and what a target resolves is a
	// plausible cause of the problem, so quo records it rather than settling
	// it.
	held := nested(t, decodeMember(t, content, testVMMember),
		"spec", "template", "spec", "domain")
	g.Expect(held).ToNot(gomega.HaveKey("machine"))
	g.Expect(held).ToNot(gomega.HaveKey("cpu"))
}

func TestExportHoldsEvidenceApartFromTheObjectsItCarries(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	order, content := captureWithDisks(t, provisionedCluster(t), testOptions())

	manifest := capturedManifest(t, content)
	g.Expect(manifest.Objects).ToNot(gomega.BeEmpty())
	for i := range manifest.Objects {
		g.Expect(manifest.Objects[i].Path).To(gomega.HavePrefix(bundle.ObjectsDir + "/"))
	}
	g.Expect(manifest.Evidence).ToNot(gomega.BeEmpty())
	for i := range manifest.Evidence {
		g.Expect(manifest.Evidence[i].Path).To(gomega.HavePrefix(bundle.EvidenceDir + "/"))
	}
	// Carrying the VirtualMachine and its expanded form under one name is
	// only possible because the two groupings are separate.
	g.Expect(order).To(gomega.ContainElement(testVMMember))
	g.Expect(order).To(gomega.ContainElement(testExpandedMember))
	g.Expect(content[testVMMember]).ToNot(gomega.Equal(content[testExpandedMember]))
}

func TestExportRecordsCDIAsNotInstalledRatherThanTreatingItAsAnError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := provisionedCluster(t)
	// A cluster without CDI serves no StorageProfiles at all.
	c.refuse(
		clusterRef(cluster.KindStorageProfile, testStorageClass),
		fmt.Errorf("%s: %w", cluster.KindStorageProfile, cluster.ErrNotInstalled))

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// Which says something about the cluster rather than something having
	// gone wrong with the capture.
	g.Expect(manifest.CaptureRecord.Skipped()).To(gomega.Equal([]bundle.Attempt{
		{
			Kind:        cluster.KindStorageProfile,
			Name:        testStorageClass,
			Disposition: bundle.DispositionSkipped,
			Reason:      bundle.SkipNotInstalled,
			Message:     fmt.Sprintf("%s: %s", cluster.KindStorageProfile, cluster.ErrNotInstalled),
		},
	}))
	// Everything else about the source environment was still collected.
	g.Expect(gathered(manifest)).To(gomega.Equal(append(
		[]cluster.ObjectRef{
			clusterRef(cluster.KindStorageClass, testStorageClass),
			clusterRef(cluster.KindPersistentVolume, testRootPV),
		},
		sourceEvidence()...)))
	g.Expect(manifest.Disks).To(gomega.HaveLen(1))
}

func TestExportRecordsEveryForbiddenClusterScopedReadAndCapturesAnyway(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := provisionedCluster(t)
	// What a namespace owner without cluster read sees: the claim in their
	// own namespace is theirs, and nothing behind it is.
	forbidden := []cluster.ObjectRef{
		clusterRef(cluster.KindStorageClass, testStorageClass),
		clusterRef(cluster.KindStorageProfile, testStorageClass),
		clusterRef(cluster.KindPersistentVolume, testRootPV),
	}
	for _, object := range forbidden {
		c.refuse(object, refusal(object.Kind, object.Name, cluster.ErrForbidden))
	}
	c.kvErr = fmt.Errorf("%s: %w", cluster.KindKubeVirt, cluster.ErrForbidden)
	c.expandErr = refusal(cluster.KindVirtualMachine, testName, cluster.ErrForbidden)

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// Failing a capture that may have taken hours over evidence nobody is
	// entitled to would be unacceptable, and omitting it in silence would be
	// worse: the reader could not tell it from a cluster that had none.
	for _, attempt := range manifest.CaptureRecord.Skipped() {
		g.Expect(attempt.Reason).To(gomega.Equal(bundle.SkipForbidden))
	}
	g.Expect(skipped(manifest)).To(gomega.Equal(append(
		forbidden,
		cluster.ObjectRef{Kind: cluster.KindKubeVirt},
		ref(cluster.KindVirtualMachine, testName))))
	g.Expect(manifest.Evidence).To(gomega.BeEmpty())
	// The bundle is still worth having: the VirtualMachine, its closure and
	// its disks all arrived.
	g.Expect(carried(manifest)).To(gomega.Equal([]cluster.ObjectRef{
		ref(cluster.KindVirtualMachine, testName),
		ref(cluster.KindPersistentVolumeClaim, rootClaim),
	}))
	g.Expect(manifest.Disks).To(gomega.HaveLen(1))
}

func TestExportCapturesAStorageClassTwoClaimsShareOnce(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := provisionedCluster(t)
	attach(t, c.vm, []volume{
		{name: "rootdisk", claim: rootClaim},
		{name: "datadisk", claim: dataClaim},
	})
	provision(t, c, dataClaim, map[string]any{
		"storageClassName": testStorageClass,
		"volumeName":       testDataPV,
	})
	c.hold(clusterRef(cluster.KindPersistentVolume, testDataPV), nil)
	c.offer(dataClaim)

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	g.Expect(gathered(manifest)).To(gomega.Equal(append(
		[]cluster.ObjectRef{
			clusterRef(cluster.KindStorageClass, testStorageClass),
			clusterRef(cluster.KindStorageProfile, testStorageClass),
			clusterRef(cluster.KindPersistentVolume, testRootPV),
			clusterRef(cluster.KindPersistentVolume, testDataPV),
		},
		sourceEvidence()...)))
	// Read from the cluster once, so recorded once.
	g.Expect(c.requested).To(gomega.Equal([]cluster.ObjectRef{
		ref(cluster.KindPersistentVolumeClaim, rootClaim),
		ref(cluster.KindPersistentVolumeClaim, dataClaim),
		clusterRef(cluster.KindStorageClass, testStorageClass),
		clusterRef(cluster.KindStorageProfile, testStorageClass),
		clusterRef(cluster.KindPersistentVolume, testRootPV),
		clusterRef(cluster.KindPersistentVolume, testDataPV),
	}))
}

func TestExportSeeksNoStorageEvidenceAClaimDoesNotName(t *testing.T) {
	tests := []struct {
		name string
		// spec is the claim's spec as the cluster holds it.
		spec map[string]any
		// unreadable makes the claim itself one quo cannot read.
		unreadable bool
		want       []cluster.ObjectRef
	}{
		{
			name: "a claim provisioned without a storage class names no class and no profile",
			spec: map[string]any{"volumeName": testRootPV},
			want: []cluster.ObjectRef{clusterRef(cluster.KindPersistentVolume, testRootPV)},
		},
		{
			name: "a claim that is not yet bound names no volume",
			spec: map[string]any{"storageClassName": testStorageClass},
			want: []cluster.ObjectRef{
				clusterRef(cluster.KindStorageClass, testStorageClass),
				clusterRef(cluster.KindStorageProfile, testStorageClass),
			},
		},
		{
			// Guessing at what was behind it would put an absence in the
			// bundle that reads as a finding.
			name:       "a claim quo was not allowed to read names nothing at all",
			unreadable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			c := provisionedCluster(t)
			provision(t, c, rootClaim, tt.spec)
			if tt.unreadable {
				c.refuse(
					ref(cluster.KindPersistentVolumeClaim, rootClaim),
					refusal(cluster.KindPersistentVolumeClaim, rootClaim, cluster.ErrForbidden))
			}

			_, content := captureWithDisks(t, c, testOptions())

			manifest := capturedManifest(t, content)
			g.Expect(gathered(manifest)).To(gomega.Equal(append(tt.want, sourceEvidence()...)))
		})
	}
}

// provisionedCluster builds a cluster whose stopped VirtualMachine has one
// disk, backed by a claim bound to a PersistentVolume of a StorageClass the
// cluster holds along with its CDI StorageProfile.
func provisionedCluster(t *testing.T) (c *fakeCluster) {
	t.Helper()
	c = stoppedCluster(t)
	attach(t, c.vm, []volume{{name: "rootdisk", claim: rootClaim}})
	provision(t, c, rootClaim, map[string]any{
		"storageClassName": testStorageClass,
		"volumeName":       testRootPV,
	})
	c.hold(clusterRef(cluster.KindStorageClass, testStorageClass), map[string]any{
		"provisioner":       testProvisioner,
		"volumeBindingMode": testBindingMode,
		"parameters":        map[string]any{"csi.storage.k8s.io/fstype": "ext4"},
	})
	c.hold(clusterRef(cluster.KindStorageProfile, testStorageClass), map[string]any{
		"status": map[string]any{
			"provisioner": testProvisioner,
			"claimPropertySets": []any{
				map[string]any{
					"accessModes": []any{"ReadWriteMany"},
					"volumeMode":  testVolumeMode,
				},
			},
		},
	})
	c.hold(clusterRef(cluster.KindPersistentVolume, testRootPV), map[string]any{
		"spec": map[string]any{
			"capacity": map[string]any{"storage": testCapacity},
			"csi": map[string]any{
				"driver":           testProvisioner,
				"volumeAttributes": map[string]any{"imageName": "csi-vol-9e1f"},
			},
		},
	})
	c.offer(rootClaim)
	return
}

// provision puts a PersistentVolumeClaim with the given spec in the cluster,
// replacing whatever was there under that name.
func provision(t *testing.T, c *fakeCluster, name string, spec map[string]any) {
	t.Helper()
	held := map[string]any{}
	if spec != nil {
		held["spec"] = spec
	}
	c.hold(ref(cluster.KindPersistentVolumeClaim, name), held)
}

// sourceEvidence is what every fixture cluster says about itself, collected
// after whatever the claims named.
func sourceEvidence() (refs []cluster.ObjectRef) {
	refs = []cluster.ObjectRef{
		refIn(cluster.KindKubeVirt, testKubeVirtNamespace, testKubeVirtName),
		ref(cluster.KindVirtualMachine, testName),
	}
	return
}

// gathered returns the evidence a manifest says the bundle holds.
func gathered(manifest *bundle.Manifest) (refs []cluster.ObjectRef) {
	for i := range manifest.Evidence {
		object := manifest.Evidence[i]
		refs = append(refs, cluster.ObjectRef{
			Kind:      object.Kind,
			Namespace: object.Namespace,
			Name:      object.Name,
		})
	}
	return
}

// skipped returns the objects a capture record says were not collected.
func skipped(manifest *bundle.Manifest) (refs []cluster.ObjectRef) {
	for _, attempt := range manifest.CaptureRecord.Skipped() {
		refs = append(refs, cluster.ObjectRef{
			Kind:      attempt.Kind,
			Namespace: attempt.Namespace,
			Name:      attempt.Name,
		})
	}
	return
}
