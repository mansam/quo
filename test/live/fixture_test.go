//go:build live

package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/mansam/quo/pkg/capture"
	"github.com/mansam/quo/pkg/cluster"
)

// Resources the fixture creates.
var (
	namespaceResource = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "namespaces",
	}
	configMapResource = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "configmaps",
	}
	secretResource = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "secrets",
	}
	serviceAccountResource = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "serviceaccounts",
	}
	dataVolumeResource = schema.GroupVersionResource{
		Group:    "cdi.kubevirt.io",
		Version:  "v1beta1",
		Resource: "datavolumes",
	}
	virtualMachineResource = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachines",
	}
	instancetypeResource = schema.GroupVersionResource{
		Group:    "instancetype.kubevirt.io",
		Version:  "v1beta1",
		Resource: "virtualmachineinstancetypes",
	}
	preferenceResource = schema.GroupVersionResource{
		Group:    "instancetype.kubevirt.io",
		Version:  "v1beta1",
		Resource: "virtualmachinepreferences",
	}
	attachmentResource = schema.GroupVersionResource{
		Group:    "k8s.cni.cncf.io",
		Version:  "v1",
		Resource: "network-attachment-definitions",
	}
)

// attachmentGroupVersion is where Multus serves
// NetworkAttachmentDefinitions, which an installation without Multus does
// not serve at all.
const attachmentGroupVersion = "k8s.cni.cncf.io/v1"

// Parts of the fixture. Each one names an object the VirtualMachine refers
// to, so that a reference type quo's traversal follows is exercised against a
// real cluster rather than only against the fake.
const (
	partNamespace    = "ns"
	partVM           = "vm"
	partDisk         = "root"
	partConfig       = "config"
	partCloudInit    = "cloudinit"
	partCredentials  = "keys"
	partAccount      = "reader"
	partInstancetype = "size"
	partPreference   = "guest"
	partAttachment   = "net"
)

// Volumes of the fixture VirtualMachine, in the order it declares them. Each
// one is also a disk, because KubeVirt requires every volume to be attached
// to one.
const (
	volumeDisk      = "rootdisk"
	volumeContainer = "containerdisk"
	volumeConfig    = "config"
	volumeCloudInit = "cloudinit"
	volumeAccount   = "serviceaccount"
)

// volumeNames are the fixture's volumes in declaration order.
var volumeNames = []string{
	volumeDisk,
	volumeContainer,
	volumeConfig,
	volumeCloudInit,
	volumeAccount,
}

// Networks of the fixture VirtualMachine.
const (
	networkPod       = "default"
	networkSecondary = "secondary"
)

// What the fixture is made of.
const (
	// fixtureDiskSize is what the fixture disk asks for. It is blank and so
	// almost entirely sparse, which means it compresses to very little on the
	// way into a bundle, but it has to be large enough that any provisioner
	// will take it.
	fixtureDiskSize = "1Gi"
	// fixtureImage is the container image one volume is served from. Nothing
	// ever pulls it: the VirtualMachine is never started, and quo declares a
	// container disk as a requirement rather than carrying it.
	fixtureImage = "quay.io/kubevirt/cirros-container-disk-demo:latest"
	// bindImmediately tells CDI to provision the fixture disk at once rather
	// than waiting for something to consume it.
	//
	// A storage class that binds on first consumer leaves a DataVolume
	// waiting in WaitForFirstConsumer until a pod mounts the claim behind it,
	// and the fixture VirtualMachine is deliberately never started, so
	// nothing ever would. The annotation has CDI schedule a worker of its own
	// and bind, which is what it is for.
	bindImmediately = "cdi.kubevirt.io/storage.bind.immediate.requested"
	// Identity values a bundle must carry through untouched, which the export
	// test reads back out of the bundle.
	fixtureUUID   = "5d307ca9-b3ef-428c-8861-06e72d69f223"
	fixtureSerial = "SN-00042"
)

// How long the fixture waits for the cluster.
const (
	// provisionTimeout bounds the wait for CDI to provision the fixture disk.
	provisionTimeout = 10 * time.Minute
	// settleTimeout bounds the wait for KubeVirt to finish filling in the
	// VirtualMachine it has just been given.
	settleTimeout = 5 * time.Minute
	// deleteTimeout bounds taking the fixture down again.
	deleteTimeout = 5 * time.Minute
	// pollInterval is how often the fixture reads back what it waits for.
	pollInterval = 2 * time.Second
)

// fixture is the VirtualMachine the suite captures and everything it refers
// to.
//
// It is built rather than found, so that a run accounts for every reference
// type quo follows and a failure names a shape the suite put there on
// purpose. Every name is derived from one random suffix, so that two runs
// sharing a namespace do not collide.
type fixture struct {
	namespace string
	suffix    string
	// attached names the NetworkAttachmentDefinition the VirtualMachine's
	// secondary network points at, empty where Multus is not installed.
	attached string
	// created is everything the fixture created, removed in the reverse order.
	created []created
}

// created is one object the fixture created, so that everything it created
// is taken down again whether or not the rest of the build succeeded.
type created struct {
	gvr       schema.GroupVersionResource
	namespace string
	name      string
}

// newFixture names the objects a run will create. Nothing is created yet: a
// fixture is named before the cluster is known to be usable.
func newFixture() (f *fixture) {
	f = &fixture{
		namespace: os.Getenv(envNamespace),
		suffix:    suffix(),
	}
	return
}

// named is what the fixture calls one of its objects.
func (r *fixture) named(part string) (name string) {
	name = "quo-live-" + r.suffix + "-" + part
	return
}

// ref names one of the fixture's objects the way quo refers to it.
func (r *fixture) ref(kind, part string) (ref cluster.ObjectRef) {
	ref = cluster.ObjectRef{
		Kind:      kind,
		Namespace: r.namespace,
		Name:      r.named(part),
	}
	return
}

// create builds the fixture on the cluster and waits for it to settle.
//
// The disk is provisioned before the VirtualMachine is created, and the
// VirtualMachine is waited for until KubeVirt has resolved its instancetype,
// so that both traversals read a VirtualMachine that has stopped changing. A
// VirtualMachine still being filled in would have the two reading different
// objects and disagreeing over something neither implementation got wrong.
func (r *fixture) create(ctx context.Context, s *suite) (err error) {
	err = r.createNamespace(ctx, s)
	if err != nil {
		return
	}
	err = r.createReferenced(ctx, s)
	if err != nil {
		return
	}
	err = r.createDisk(ctx, s)
	if err != nil {
		return
	}
	err = r.createVirtualMachine(ctx, s)
	return
}

// delete takes down everything the fixture created, in the reverse of the
// order it created it, which leaves a namespace the fixture made until last.
//
// A removal that fails is reported and does not stop the rest. Leaving one
// object behind is a nuisance; stopping would leave the others behind too.
// The context is detached from the one the run used, because a run that was
// cancelled still has a fixture to take down.
func (r *fixture) delete(ctx context.Context, s *suite) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteTimeout)
	defer cancel()
	for i := len(r.created) - 1; i >= 0; i-- {
		writer := s.writerFor(r.created[i].gvr, r.created[i].namespace)
		err := writer.Delete(ctx, r.created[i].name, meta.DeleteOptions{})
		if err == nil || k8serr.IsNotFound(err) {
			continue
		}
		_, _ = fmt.Fprintf(
			os.Stderr,
			"live suite: delete %s %q: %v\n",
			r.created[i].gvr.Resource,
			r.created[i].name,
			err)
	}
}

// createNamespace creates the namespace the fixture lives in, unless the run
// was given one to use.
func (r *fixture) createNamespace(ctx context.Context, s *suite) (err error) {
	if r.namespace != "" {
		return
	}
	r.namespace = r.named(partNamespace)
	err = r.submit(ctx, s, namespaceResource, map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": r.namespace},
	})
	return
}

// createReferenced creates the objects the VirtualMachine refers to, other
// than the disk.
func (r *fixture) createReferenced(ctx context.Context, s *suite) (err error) {
	err = r.submit(ctx, s, configMapResource, r.buildObject("v1", "ConfigMap", partConfig, map[string]any{
		"data": map[string]any{"autounattend.xml": "<unattend/>"},
	}))
	if err != nil {
		return
	}
	err = r.submit(ctx, s, secretResource, r.buildObject("v1", "Secret", partCloudInit, map[string]any{
		"stringData": map[string]any{"userdata": "#cloud-config\n"},
	}))
	if err != nil {
		return
	}
	err = r.submit(ctx, s, secretResource, r.buildObject("v1", "Secret", partCredentials, map[string]any{
		"stringData": map[string]any{"key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 quo-live"},
	}))
	if err != nil {
		return
	}
	err = r.submit(ctx, s, serviceAccountResource, r.buildObject("v1", "ServiceAccount", partAccount, nil))
	if err != nil {
		return
	}
	err = r.submit(ctx, s, instancetypeResource, r.buildObject(
		"instancetype.kubevirt.io/v1beta1",
		"VirtualMachineInstancetype",
		partInstancetype,
		map[string]any{
			"spec": map[string]any{
				"cpu":    map[string]any{"guest": int64(1)},
				"memory": map[string]any{"guest": "128Mi"},
			},
		}))
	if err != nil {
		return
	}
	err = r.submit(ctx, s, preferenceResource, r.buildObject(
		"instancetype.kubevirt.io/v1beta1",
		"VirtualMachinePreference",
		partPreference,
		map[string]any{
			"spec": map[string]any{
				"devices": map[string]any{"preferredDiskBus": "virtio"},
			},
		}))
	if err != nil {
		return
	}
	err = r.createAttachment(ctx, s)
	return
}

// createAttachment creates the NetworkAttachmentDefinition the
// VirtualMachine's secondary network points at.
//
// Multus is optional and a cluster without it serves no such kind. That is
// not a reason to skip the suite: the VirtualMachine is built without a
// secondary network instead, and the comparison covers one reference type
// fewer.
func (r *fixture) createAttachment(ctx context.Context, s *suite) (err error) {
	if !s.serves(attachmentGroupVersion, attachmentResource.Resource) {
		return
	}
	name := r.named(partAttachment)
	// Multus requires the name inside the config to be the object's own.
	config := fmt.Sprintf(
		`{"cniVersion":"0.3.1","name":%q,"type":"bridge","bridge":"br-quo-live"}`,
		name)
	err = r.submit(ctx, s, attachmentResource, r.buildObject(
		attachmentGroupVersion,
		"NetworkAttachmentDefinition",
		partAttachment,
		map[string]any{
			"spec": map[string]any{"config": config},
		}))
	if err != nil {
		return
	}
	r.attached = name
	return
}

// createDisk creates the DataVolume behind the VirtualMachine's root disk and
// waits for CDI to provision it, so that the claim exists and is bound before
// anything tries to export it.
func (r *fixture) createDisk(ctx context.Context, s *suite) (err error) {
	dv := r.buildObject(
		"cdi.kubevirt.io/v1beta1",
		"DataVolume",
		partDisk,
		map[string]any{
			"spec": map[string]any{
				"source":  map[string]any{"blank": map[string]any{}},
				"storage": diskStorage(),
			},
		})
	annotate(dv, bindImmediately, "true")
	err = r.submit(ctx, s, dataVolumeResource, dv)
	if err != nil {
		return
	}
	_, err = s.await(
		ctx,
		dataVolumeResource,
		r.namespace,
		r.named(partDisk),
		"become provisioned",
		provisionTimeout,
		provisioned)
	return
}

// diskStorage is what the fixture asks of the cluster's storage for its
// disk: fixtureDiskSize on the class the environment named, or on
// whichever class the cluster defaults to, as a block device. The volume
// mode is asked for by name rather than left to the class's default,
// because the export offers a block device as a gzip and a filesystem
// volume as a tar.gz, and the tar.gz the export produces is not a form
// every cluster's CDI takes back — the round trip runs on the gzip form,
// and the deploy suite's disk fixtures cover the tar.gz bytes'
// pass-through.
func diskStorage() (storage map[string]any) {
	storage = map[string]any{
		"resources": map[string]any{
			"requests": map[string]any{"storage": fixtureDiskSize},
		},
		"volumeMode": "Block",
	}
	class := os.Getenv(envStorageClass)
	if class != "" {
		storage["storageClassName"] = class
	}
	return
}

// createVirtualMachine creates the VirtualMachine the suite captures.
//
// It is halted and never started. quo refuses a VirtualMachine that is not
// stopped, and a stopped one is also what makes the comparison meaningful:
// the VirtualMachineInstance and virt-launcher Pod KubeVirt's graph would
// otherwise name are the objects quo deliberately does not follow.
func (r *fixture) createVirtualMachine(ctx context.Context, s *suite) (err error) {
	err = r.submit(ctx, s, virtualMachineResource, r.buildObject(
		"kubevirt.io/v1",
		"VirtualMachine",
		partVM,
		map[string]any{"spec": r.buildVirtualMachineSpec()}))
	if err != nil {
		return
	}
	_, err = s.await(
		ctx,
		virtualMachineResource,
		r.namespace,
		r.named(partVM),
		"settle",
		settleTimeout,
		settled)
	return
}

// buildVirtualMachineSpec returns the fixture VirtualMachine's spec. Every
// volume, matcher and network on it is a reference type quo's traversal
// follows, so that the comparison against KubeVirt's graph has something to
// compare.
func (r *fixture) buildVirtualMachineSpec() (spec map[string]any) {
	spec = map[string]any{
		"runStrategy": "Halted",
		// The template declares the root disk the way a machine that ran
		// once and was stopped does: KubeVirt provisions a template's
		// DataVolume when the machine starts and leaves it behind when it
		// stops. The fixture never starts the machine, so it creates the
		// DataVolume itself, and the template and the volume name one and
		// the same disk — which is what gives the round trip a template to
		// hoist onto a standalone DataVolume.
		"dataVolumeTemplates": []any{r.buildDataVolumeTemplate()},
		"instancetype": map[string]any{
			"kind": "VirtualMachineInstancetype",
			"name": r.named(partInstancetype),
		},
		"preference": map[string]any{
			"kind": "VirtualMachinePreference",
			"name": r.named(partPreference),
		},
		"template": map[string]any{
			"spec": map[string]any{
				"accessCredentials": []any{
					map[string]any{
						"sshPublicKey": map[string]any{
							"source": map[string]any{
								"secret": map[string]any{
									"secretName": r.named(partCredentials),
								},
							},
							"propagationMethod": map[string]any{
								"noCloud": map[string]any{},
							},
						},
					},
				},
				"domain": map[string]any{
					"devices": map[string]any{
						"disks":      r.buildDisks(),
						"interfaces": r.buildInterfaces(),
					},
					// Identity the capture must carry through untouched. Any
					// of these could be the thing causing the problem a
					// bundle was captured for.
					"firmware": map[string]any{
						"uuid":   fixtureUUID,
						"serial": fixtureSerial,
					},
				},
				"networks": r.buildNetworks(),
				"volumes":  r.buildVolumes(),
			},
		},
	}
	return
}

// buildDataVolumeTemplate returns the template stanza declaring the root
// disk: the same name, source and storage as the DataVolume the fixture
// creates for it.
func (r *fixture) buildDataVolumeTemplate() (template map[string]any) {
	template = map[string]any{
		"metadata": map[string]any{"name": r.named(partDisk)},
		"spec": map[string]any{
			"source":  map[string]any{"blank": map[string]any{}},
			"storage": diskStorage(),
		},
	}
	return
}

// buildVolumes returns the fixture VirtualMachine's volumes: one backed by a
// DataVolume, one served from a container image, and one each for the
// ConfigMap, the cloud-init Secret and the ServiceAccount.
func (r *fixture) buildVolumes() (volumes []any) {
	volumes = []any{
		map[string]any{
			"name":       volumeDisk,
			"dataVolume": map[string]any{"name": r.named(partDisk)},
		},
		map[string]any{
			"name":          volumeContainer,
			"containerDisk": map[string]any{"image": fixtureImage},
		},
		map[string]any{
			"name":      volumeConfig,
			"configMap": map[string]any{"name": r.named(partConfig)},
		},
		map[string]any{
			"name": volumeCloudInit,
			"cloudInitNoCloud": map[string]any{
				"secretRef": map[string]any{"name": r.named(partCloudInit)},
			},
		},
		map[string]any{
			"name":           volumeAccount,
			"serviceAccount": map[string]any{"serviceAccountName": r.named(partAccount)},
		},
	}
	return
}

// buildDisks attaches every volume to a disk, which KubeVirt requires.
func (r *fixture) buildDisks() (disks []any) {
	for _, name := range volumeNames {
		disks = append(disks, map[string]any{
			"name": name,
			"disk": map[string]any{"bus": "virtio"},
		})
	}
	return
}

// buildNetworks returns the pod network every VirtualMachine has and, where
// Multus is installed, a secondary network naming the fixture's
// NetworkAttachmentDefinition.
func (r *fixture) buildNetworks() (networks []any) {
	networks = []any{
		map[string]any{"name": networkPod, "pod": map[string]any{}},
	}
	if r.attached == "" {
		return
	}
	networks = append(networks, map[string]any{
		"name":   networkSecondary,
		"multus": map[string]any{"networkName": r.attached},
	})
	return
}

// buildInterfaces returns the interfaces binding the VirtualMachine to its
// networks, one interface each.
func (r *fixture) buildInterfaces() (interfaces []any) {
	interfaces = []any{
		map[string]any{"name": networkPod, "masquerade": map[string]any{}},
	}
	if r.attached == "" {
		return
	}
	interfaces = append(interfaces, map[string]any{
		"name":   networkSecondary,
		"bridge": map[string]any{},
	})
	return
}

// buildObject returns one fixture object, carrying whatever fields beyond its
// kind and name the thing being tested needs.
func (r *fixture) buildObject(
	apiVersion, kind, part string,
	fields map[string]any) (object map[string]any) {
	//
	object = map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":      r.named(part),
			"namespace": r.namespace,
		},
	}
	for key, value := range fields {
		object[key] = value
	}
	return
}

// annotate puts one annotation on an object the fixture is about to create.
func annotate(object map[string]any, name, value string) {
	metadata, _ := object["metadata"].(map[string]any)
	annotations, found := metadata["annotations"].(map[string]any)
	if !found {
		annotations = map[string]any{}
		metadata["annotations"] = annotations
	}
	annotations[name] = value
}

// submit sends one object to the cluster and records it, so that everything
// the fixture created is taken down again whether or not the rest of the
// build succeeds.
func (r *fixture) submit(
	ctx context.Context,
	s *suite,
	gvr schema.GroupVersionResource,
	object map[string]any) (err error) {
	//
	built := &unstructured.Unstructured{Object: object}
	writer := s.writerFor(gvr, built.GetNamespace())
	_, err = writer.Create(ctx, built, meta.CreateOptions{})
	if err != nil {
		err = fmt.Errorf("create %s %q: %w", built.GetKind(), built.GetName(), err)
		return
	}
	r.created = append(r.created, created{
		gvr:       gvr,
		namespace: built.GetNamespace(),
		name:      built.GetName(),
	})
	return
}

// provisioned reports a DataVolume CDI has finished importing, which is when
// the claim behind it exists and holds the blank image.
func provisioned(dv *unstructured.Unstructured) (done bool) {
	phase, _, _ := unstructured.NestedString(dv.Object, "status", "phase")
	done = phase == "Succeeded"
	return
}

// settled reports a VirtualMachine KubeVirt has finished filling in: it is
// stopped, and both the instancetype and the preference it was asked for
// have been pinned to a ControllerRevision.
//
// Both traversals read the VirtualMachine separately. One still being filled
// in would have them read different objects and disagree over a
// ControllerRevision that simply did not exist yet for one of them, which is
// a failure naming an object neither implementation got wrong.
func settled(vm *unstructured.Unstructured) (done bool) {
	status, _, _ := unstructured.NestedString(vm.Object, "status", "printableStatus")
	if status != capture.StatusStopped {
		return
	}
	for _, matcher := range []string{"instancetypeRef", "preferenceRef"} {
		pinned, _, _ := unstructured.NestedString(
			vm.Object,
			"status",
			matcher,
			"controllerRevisionRef",
			"name")
		if pinned == "" {
			return
		}
	}
	done = true
	return
}

// suffix is what keeps two runs in one namespace from colliding.
func suffix() (s string) {
	value := make([]byte, 4)
	_, _ = rand.Read(value)
	s = hex.EncodeToString(value)
	return
}
