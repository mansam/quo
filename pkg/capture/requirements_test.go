package capture

import (
	"testing"

	"github.com/onsi/gomega"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

// The storage the fixture VirtualMachine's two disks were provisioned from,
// which differ in everything a target would have to match.
const (
	testFilesystemClass = "nfs-csi"
	testBlockSize       = "32Gi"
	testFilesystemSize  = "10Gi"
)

// The container image a fixture VirtualMachine is served from.
const testContainerImage = "quay.io/example/fedora:41"

func TestExportDeclaresTheNetworksTheVirtualMachineExpects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{"name": "default", "pod": map[string]any{}},
		map[string]any{
			"name":   "storage",
			"multus": map[string]any{"networkName": testNetwork},
		},
		map[string]any{
			"name":   "backup",
			"multus": map[string]any{"networkName": "shared-networks/backup-net"},
		},
	}, "spec", "template", "spec", "networks")
	c.hold(ref(cluster.KindNetworkAttachmentDefinition, testNetwork), nil)
	c.hold(refIn(cluster.KindNetworkAttachmentDefinition, "shared-networks", "backup-net"), nil)

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// Every cluster has a pod network, so that one names nothing. The two
	// secondary networks name the attachments the source cluster backed them
	// with, which is what a target has to be able to offer something
	// equivalent to.
	g.Expect(manifest.Requirements.Networks).To(gomega.Equal([]bundle.NetworkRequirement{
		{Name: "default", Type: bundle.NetworkPod},
		{
			Name:       "storage",
			Type:       bundle.NetworkMultus,
			Namespace:  testNamespace,
			Attachment: testNetwork,
		},
		{
			Name:       "backup",
			Type:       bundle.NetworkMultus,
			Namespace:  "shared-networks",
			Attachment: "backup-net",
		},
	}))
}

func TestExportDoesNotReportASecondaryNetworkAsOneEveryClusterAlreadyHas(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	// A Multus network naming no attachment. KubeVirt would not accept this
	// VirtualMachine, but reading it as the pod network would tell a reader
	// their cluster needs nothing when quo has no idea what it needs.
	setField(t, c.vm, []any{
		map[string]any{"name": "secondary", "multus": map[string]any{}},
	}, "spec", "template", "spec", "networks")

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	g.Expect(manifest.Requirements.Networks).To(gomega.Equal([]bundle.NetworkRequirement{
		{Name: "secondary", Type: bundle.NetworkMultus},
	}))
	// There is nothing to name, so nothing a target is sent looking for.
	g.Expect(required(manifest)).To(gomega.BeEmpty())
}

func TestExportDeclaresWhatEachDiskNeedsFromStorage(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, content := captureWithDisks(t, requiringCluster(t), testOptions())

	manifest := capturedManifest(t, content)
	// A target that can only offer one volume mode cannot run this
	// VirtualMachine as captured, so each disk says which one it needs rather
	// than leaving it to be guessed at.
	g.Expect(manifest.Requirements.Storage).To(gomega.Equal([]bundle.StorageRequirement{
		{
			Claim:        rootClaim,
			StorageClass: testStorageClass,
			Size:         testBlockSize,
			VolumeMode:   "Block",
		},
		{
			Claim:        dataClaim,
			StorageClass: testFilesystemClass,
			Size:         testFilesystemSize,
			VolumeMode:   "Filesystem",
		},
	}))
	// One requirement per disk member, so nothing the bundle carries arrives
	// without a target having been told what it would take to put it back.
	g.Expect(manifest.Disks).To(gomega.HaveLen(len(manifest.Requirements.Storage)))
}

func TestExportDeclaresTheCapacityADiskWasBoundToRatherThanTheOneItAskedFor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	attach(t, c.vm, []volume{{name: "rootdisk", claim: rootClaim}})
	c.hold(ref(cluster.KindPersistentVolumeClaim, rootClaim), map[string]any{
		"spec": map[string]any{
			"resources": map[string]any{"requests": map[string]any{"storage": "30Gi"}},
		},
		"status": map[string]any{"capacity": map[string]any{"storage": testBlockSize}},
	})
	c.offer(rootClaim)

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// A target given the smaller of the two has nowhere to put the disk.
	g.Expect(manifest.Requirements.Storage).To(gomega.Equal([]bundle.StorageRequirement{
		{Claim: rootClaim, Size: testBlockSize},
	}))
}

func TestExportDeclaresADiskWhoseClaimItCouldNotReadWithWhatItKnows(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := requiringCluster(t)
	c.refuse(
		ref(cluster.KindPersistentVolumeClaim, rootClaim),
		refusal(cluster.KindPersistentVolumeClaim, rootClaim, cluster.ErrForbidden))

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// The disk is in the bundle whether or not its claim could be read, so a
	// target still has to find room for it. Saying so with the name alone
	// beats dropping the requirement and leaving a disk nothing accounts for.
	g.Expect(manifest.Requirements.Storage).To(gomega.Equal([]bundle.StorageRequirement{
		{Claim: rootClaim},
		{
			Claim:        dataClaim,
			StorageClass: testFilesystemClass,
			Size:         testFilesystemSize,
			VolumeMode:   "Filesystem",
		},
	}))
	g.Expect(manifest.Disks).To(gomega.HaveLen(2))
}

func TestExportDeclaresStorageRequirementsWhateverItCouldCollect(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := requiringCluster(t)
	// What a namespace owner without cluster read sees: the claims in their
	// own namespace are theirs, and nothing behind them is.
	for _, object := range []cluster.ObjectRef{
		clusterRef(cluster.KindStorageClass, testStorageClass),
		clusterRef(cluster.KindStorageProfile, testStorageClass),
		clusterRef(cluster.KindStorageClass, testFilesystemClass),
		clusterRef(cluster.KindStorageProfile, testFilesystemClass),
	} {
		c.refuse(object, refusal(object.Kind, object.Name, cluster.ErrForbidden))
	}

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// Requirements come off the VirtualMachine and its claims, so a capture
	// that could collect nothing about the source cluster's storage still
	// says what a target would need.
	g.Expect(collected(manifest)).To(gomega.Equal(append(
		append(fromVirtualMachine(), ref(cluster.KindPersistentVolumeClaim, dataClaim)),
		sourceCluster()...)))
	g.Expect(manifest.Requirements.Storage).To(gomega.HaveLen(2))
	g.Expect(manifest.Requirements.Storage[0].StorageClass).To(gomega.Equal(testStorageClass))
	g.Expect(manifest.Requirements.Storage[1].StorageClass).To(gomega.Equal(testFilesystemClass))
}

func TestExportDeclaresContainerDiskImagesAndNeverRepackagesThem(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{
			"name":                  "rootdisk",
			"persistentVolumeClaim": map[string]any{"claimName": rootClaim},
		},
		map[string]any{
			"name": "containerdisk",
			"containerDisk": map[string]any{
				"image":           testContainerImage,
				"imagePullSecret": testSecret,
			},
		},
	}, "spec", "template", "spec", "volumes")
	c.hold(ref(cluster.KindPersistentVolumeClaim, rootClaim), nil)
	c.hold(ref(cluster.KindSecret, testSecret), nil)
	c.offer(rootClaim)

	order, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	g.Expect(manifest.Requirements.ContainerDisks).To(gomega.Equal(
		[]bundle.ContainerDiskRequirement{
			{Volume: "containerdisk", Image: testContainerImage},
		}))
	// quo does not repackage container images. The only disk member is the
	// claim's, and the image is recorded as a name a target has to be able to
	// pull.
	g.Expect(manifest.Disks).To(gomega.Equal([]bundle.Disk{
		{
			Claim:   rootClaim,
			Path:    rootMember,
			Format:  bundle.DiskGzip,
			Volumes: []string{"rootdisk"},
		},
	}))
	g.Expect(c.export.opened).To(gomega.Equal([]string{rootClaim}))
	for _, member := range order {
		g.Expect(member).ToNot(gomega.ContainSubstring(testContainerImage))
	}
	// The Secret the image is pulled with is a real object and is carried.
	g.Expect(collected(manifest)).To(gomega.ContainElement(ref(cluster.KindSecret, testSecret)))
}

func TestExportCreatesNoExportForAVirtualMachineServedOnlyFromAContainerImage(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	backVolume(t, c.vm, "containerdisk", map[string]any{
		"containerDisk": map[string]any{"image": testContainerImage},
	})

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// Nothing a cluster could hand back as a disk, so nothing is asked for.
	g.Expect(c.export.created).To(gomega.Equal(0))
	g.Expect(manifest.Disks).To(gomega.BeEmpty())
	g.Expect(manifest.Requirements.Storage).To(gomega.BeEmpty())
	g.Expect(manifest.Requirements.ContainerDisks).To(gomega.HaveLen(1))
}

func TestExportRecordsEveryRequirementWithTheRequiredOutcome(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := requiringCluster(t)
	attachNetwork(t, c.vm, "shared-networks/"+testNetwork)
	c.hold(refIn(cluster.KindNetworkAttachmentDefinition, "shared-networks", testNetwork), nil)
	setField(t, c.vm, []any{
		map[string]any{
			"name":                  "rootdisk",
			"persistentVolumeClaim": map[string]any{"claimName": rootClaim},
		},
		map[string]any{
			"name":                  "datadisk",
			"persistentVolumeClaim": map[string]any{"claimName": dataClaim},
		},
		map[string]any{
			"name":          "containerdisk",
			"containerDisk": map[string]any{"image": testContainerImage},
		},
	}, "spec", "template", "spec", "volumes")

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// A reader of the record alone sees what the bundle needs as well as what
	// it holds. A container disk is named by its image, because no Kubernetes
	// object stands for one.
	g.Expect(required(manifest)).To(gomega.Equal([]cluster.ObjectRef{
		refIn(cluster.KindNetworkAttachmentDefinition, "shared-networks", testNetwork),
		clusterRef(cluster.KindStorageClass, testStorageClass),
		clusterRef(cluster.KindStorageClass, testFilesystemClass),
		{Kind: bundle.KindContainerDisk, Name: testContainerImage},
	}))
}

func TestExportRecordsAStorageClassTwoDisksShareAsOneRequirement(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := requiringCluster(t)
	claimed(t, c, dataClaim, testStorageClass, testFilesystemSize, "Block")

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// Both disks declare what they need, and the one class behind them is
	// something a target provides once.
	g.Expect(manifest.Requirements.Storage).To(gomega.HaveLen(2))
	g.Expect(required(manifest)).To(gomega.Equal([]cluster.ObjectRef{
		clusterRef(cluster.KindStorageClass, testStorageClass),
	}))
}

func TestExportRecordsNoRequirementForWhatEveryClusterAlreadyHas(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	c := stoppedCluster(t)
	setField(t, c.vm, []any{
		map[string]any{"name": "default", "pod": map[string]any{}},
	}, "spec", "template", "spec", "networks")

	_, content := captureWithDisks(t, c, testOptions())

	manifest := capturedManifest(t, content)
	// The pod network is still declared, because a reader has to know the
	// VirtualMachine expects one, but it names no object a target has to go
	// and find.
	g.Expect(manifest.Requirements.Networks).To(gomega.Equal([]bundle.NetworkRequirement{
		{Name: "default", Type: bundle.NetworkPod},
	}))
	g.Expect(required(manifest)).To(gomega.BeEmpty())
}

// requiringCluster builds a cluster whose stopped VirtualMachine has two
// disks provisioned differently: one block volume and one filesystem volume,
// of different sizes and from different storage classes.
func requiringCluster(t *testing.T) (c *fakeCluster) {
	t.Helper()
	c = stoppedCluster(t)
	attach(t, c.vm, []volume{
		{name: "rootdisk", claim: rootClaim},
		{name: "datadisk", claim: dataClaim},
	})
	claimed(t, c, rootClaim, testStorageClass, testBlockSize, "Block")
	claimed(t, c, dataClaim, testFilesystemClass, testFilesystemSize, "Filesystem")
	c.offer(rootClaim)
	c.offer(dataClaim)
	return
}

// claimed puts a bound PersistentVolumeClaim in the cluster, replacing
// whatever was there under that name.
func claimed(t *testing.T, c *fakeCluster, name, class, size, mode string) {
	t.Helper()
	c.hold(ref(cluster.KindPersistentVolumeClaim, name), map[string]any{
		"spec": map[string]any{
			"storageClassName": class,
			"volumeMode":       mode,
			"resources":        map[string]any{"requests": map[string]any{"storage": size}},
		},
		"status": map[string]any{"capacity": map[string]any{"storage": size}},
	})
}

// required returns the objects a capture record says a target has to provide.
func required(manifest *bundle.Manifest) (refs []cluster.ObjectRef) {
	for i := range manifest.CaptureRecord {
		attempt := manifest.CaptureRecord[i]
		if attempt.Outcome != bundle.OutcomeRequired {
			continue
		}
		refs = append(refs, cluster.ObjectRef{
			Kind:      attempt.Kind,
			Namespace: attempt.Namespace,
			Name:      attempt.Name,
		})
	}
	return
}
