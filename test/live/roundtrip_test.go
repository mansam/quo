//go:build live

package live

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/onsi/gomega"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/capture"
	"github.com/mansam/quo/pkg/cluster"
	"github.com/mansam/quo/pkg/deploy"
)

// partTarget names the namespace the round trip deploys into.
const partTarget = "target"

// How long the round trip waits for the cluster.
const (
	// claimGoneTimeout bounds the wait for the claim behind a deleted
	// DataVolume to be collected. The cluster's own garbage collection
	// does that work, not quo.
	claimGoneTimeout = 2 * time.Minute
)

// claimResource is how the suite reads the claim CDI makes for a DataVolume
// it created.
var claimResource = schema.GroupVersionResource{
	Version:  "v1",
	Resource: "persistentvolumeclaims",
}

// gvrByKind is how the suite reads back what a deploy created, by kind. It
// holds the kinds a deploy can leave in a namespace. Each kind maps to
// the resource serving it.
var gvrByKind = map[string]schema.GroupVersionResource{
	cluster.KindConfigMap:      configMapResource,
	cluster.KindDataVolume:     dataVolumeResource,
	cluster.KindSecret:         secretResource,
	cluster.KindServiceAccount: serviceAccountResource,
	cluster.KindVirtualMachine: virtualMachineResource,
}

func TestTheDeployedSetIsWhatTheBundleHoldsOfTheKindsADeployCreates(t *testing.T) {
	tests := []struct {
		name      string
		resources []bundle.Resource
		disks     []bundle.Disk
		want      []cluster.ObjectRef
	}{
		{
			name: "the machine, its closure and its disks, in the order the bundle holds them",
			resources: []bundle.Resource{
				{Kind: cluster.KindVirtualMachine, Namespace: "support-cases", Name: "db-01"},
				{Kind: cluster.KindPersistentVolumeClaim, Namespace: "support-cases", Name: "db-01-root"},
				{Kind: cluster.KindConfigMap, Namespace: "support-cases", Name: "db-01-sysprep"},
				{Kind: cluster.KindVirtualMachineInstancetype, Namespace: "support-cases", Name: "standard-large"},
				{Kind: cluster.KindSecret, Namespace: "support-cases", Name: "db-01-cloud-init"},
				{Kind: cluster.KindServiceAccount, Namespace: "support-cases", Name: "db-01-serviceaccount"},
				{Kind: cluster.KindDataVolume, Namespace: "support-cases", Name: "db-01-root"},
			},
			// The bundle holds the root disk's DataVolume as a resource.
			// It holds the claim as a disk. The set holds the DataVolume
			// once.
			disks: []bundle.Disk{{Claim: "db-01-root"}},
			want: []cluster.ObjectRef{
				{Kind: cluster.KindVirtualMachine, Namespace: "diagnostics", Name: "db-01"},
				{Kind: cluster.KindConfigMap, Namespace: "diagnostics", Name: "db-01-sysprep"},
				{Kind: cluster.KindSecret, Namespace: "diagnostics", Name: "db-01-cloud-init"},
				{Kind: cluster.KindServiceAccount, Namespace: "diagnostics", Name: "db-01-serviceaccount"},
				{Kind: cluster.KindDataVolume, Namespace: "diagnostics", Name: "db-01-root"},
			},
		},
		{
			// None of these lands on the target. The instancetype and
			// the revision are folded into the expansion a deploy
			// applies. The claim is created by CDI behind the
			// DataVolume. The NetworkAttachmentDefinition is mapped
			// rather than copied. The storage class describes the source
			// cluster.
			name: "a bundle holding only what a deploy reads",
			resources: []bundle.Resource{
				{Kind: cluster.KindVirtualMachineInstancetype, Namespace: "support-cases", Name: "standard-large"},
				{Kind: cluster.KindControllerRevision, Namespace: "support-cases", Name: "db-01-4f5e6d7c"},
				{Kind: cluster.KindPersistentVolumeClaim, Namespace: "support-cases", Name: "db-01-root"},
				{Kind: cluster.KindNetworkAttachmentDefinition, Namespace: "support-cases", Name: "lab-net"},
				{Kind: cluster.KindStorageClass, Name: "fast-rbd"},
			},
		},
		{
			// A disk whose claim was made by hand. The bundle holds it
			// as a claim alone. The deploy places it as a DataVolume of
			// its own, named after the claim.
			name: "a disk whose claim the bundle holds with no DataVolume",
			resources: []bundle.Resource{
				{Kind: cluster.KindVirtualMachine, Namespace: "support-cases", Name: "db-01"},
				{Kind: cluster.KindPersistentVolumeClaim, Namespace: "support-cases", Name: "db-01-scratch"},
			},
			disks: []bundle.Disk{{Claim: "db-01-scratch"}},
			want: []cluster.ObjectRef{
				{Kind: cluster.KindVirtualMachine, Namespace: "diagnostics", Name: "db-01"},
				{Kind: cluster.KindDataVolume, Namespace: "diagnostics", Name: "db-01-scratch"},
			},
		},
		{
			name:      "a bundle holding nothing at all",
			resources: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			manifest := &bundle.Manifest{Resources: tt.resources, Disks: tt.disks}

			g.Expect(deployedSet(manifest, "diagnostics")).To(gomega.Equal(tt.want))
		})
	}
}

func TestDifferencesNameWhereTwoResolutionsDisagree(t *testing.T) {
	tests := []struct {
		name   string
		held   map[string]any
		target map[string]any
		want   []string
	}{
		{
			name: "two resolutions that agree",
			held: map[string]any{
				"spec": map[string]any{"running": false},
			},
			target: map[string]any{
				"spec": map[string]any{"running": false},
			},
		},
		{
			name: "a value one cluster resolved and the other did not",
			held: map[string]any{
				"spec": map[string]any{"running": false},
			},
			target: map[string]any{
				"spec": map[string]any{"running": true},
			},
			want: []string{
				"spec.running: false in the bundle's expansion and true in the target's",
			},
		},
		{
			name: "a field the bundle's expansion carries and the target's does not",
			held: map[string]any{
				"spec": map[string]any{
					"dataVolumeTemplates": []any{map[string]any{"name": "root"}},
				},
			},
			target: map[string]any{"spec": map[string]any{}},
			want:   []string{"spec.dataVolumeTemplates: carried by the bundle's expansion and not the target's"},
		},
		{
			name: "a field the target's expansion carries and the bundle's does not",
			held: map[string]any{"spec": map[string]any{}},
			target: map[string]any{
				"spec": map[string]any{"evictionStrategy": "LiveMigrate"},
			},
			want: []string{"spec.evictionStrategy: carried by the target's expansion and not the bundle's"},
		},
		{
			name: "a difference nested in a list names the entry it is in",
			held: map[string]any{
				"spec": map[string]any{"volumes": []any{
					map[string]any{"name": "root"},
					map[string]any{"name": "data"},
				}},
			},
			target: map[string]any{
				"spec": map[string]any{"volumes": []any{
					map[string]any{"name": "root"},
					map[string]any{"name": "scratch"},
				}},
			},
			want: []string{"spec.volumes[1].name: data in the bundle's expansion and scratch in the target's"},
		},
		{
			name: "a list the two resolutions hold at different lengths",
			held: map[string]any{
				"spec": map[string]any{"volumes": []any{
					map[string]any{"name": "root"},
					map[string]any{"name": "data"},
				}},
			},
			target: map[string]any{
				"spec": map[string]any{"volumes": []any{
					map[string]any{"name": "root"},
				}},
			},
			want: []string{
				"spec.volumes: 2 entries in the bundle's expansion and 1 in the target's",
			},
		},
		{
			name: "a field whose two values are of kinds that print differently",
			held: map[string]any{
				"spec": map[string]any{"cpu": int64(4)},
			},
			target: map[string]any{
				"spec": map[string]any{"cpu": "four"},
			},
			want: []string{"spec.cpu: 4 in the bundle's expansion and four in the target's"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			g.Expect(differences(tt.held, tt.target)).To(gomega.Equal(tt.want))
		})
	}
}

// deployedSet is what a deploy of the bundle puts into a namespace. It
// holds every object the bundle holds of a kind a deploy creates, under
// the name it was captured with. It holds one DataVolume for each disk it
// places, named after the claim the disk backs. A disk whose claim the
// bundle holds without a DataVolume of its own is placed as one all the
// same. The round trip compares the target against the set. The undeploy
// at the end of one takes exactly the set off again. So one list answers
// both directions.
//
// The claim behind a disk is not among the kinds, although the target ends
// up holding one. CDI creates it behind the DataVolume. The round trip
// checks it where it checks that the disk arrived populated.
func deployedSet(manifest *bundle.Manifest, namespace string) (refs []cluster.ObjectRef) {
	created := make(map[string]bool, len(cluster.DeployedKinds))
	for _, kind := range cluster.DeployedKinds {
		created[kind] = true
	}
	seen := map[cluster.ObjectRef]bool{}
	for i := range manifest.Resources {
		ref := cluster.ObjectRef{
			Kind:      manifest.Resources[i].Kind,
			Namespace: namespace,
			Name:      manifest.Resources[i].Name,
		}
		if !created[ref.Kind] || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	// A disk the bundle holds as a DataVolume names the same object here,
	// which the set holds once.
	for i := range manifest.Disks {
		ref := cluster.ObjectRef{
			Kind:      cluster.KindDataVolume,
			Namespace: namespace,
			Name:      manifest.Disks[i].Claim,
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return
}

// differences names where two objects disagree. It gives one line per
// difference. Each line gives the path into the object, what the bundle's
// expansion holds there, and what the target's does.
//
// The round trip diffs the target's own expansion of the deployed machine
// against the expansion the bundle holds. The two clusters resolving the
// machine differently is information. The defaults one fills in and the
// other does not are exactly what a support engineer wants to know about a
// diagnostic cluster. So every difference is reported, and none is a
// failure.
func differences(held, target map[string]any) (diffs []string) {
	diffs = diffMaps("", held, target)
	return
}

// diffMaps compares the fields two objects hold, reporting each difference
// under its path. at is the path the object itself sits at, empty at the
// root.
func diffMaps(at string, held, target map[string]any) (diffs []string) {
	keys := map[string]bool{}
	for key := range held {
		keys[key] = true
	}
	for key := range target {
		keys[key] = true
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		path := key
		if at != "" {
			path = at + "." + key
		}
		heldValue, heldThere := held[key]
		targetValue, targetThere := target[key]
		switch {
		case heldThere && !targetThere:
			diffs = append(diffs, fmt.Sprintf(
				"%s: carried by the bundle's expansion and not the target's", path))
		case !heldThere && targetThere:
			diffs = append(diffs, fmt.Sprintf(
				"%s: carried by the target's expansion and not the bundle's", path))
		default:
			diffs = append(diffs, diffValues(path, heldValue, targetValue)...)
		}
	}
	return
}

// diffValues compares what two values hold at one path. Two values are
// taken to agree when they print the same. So a number one side decoded
// as an integer and the other as a float is not reported as a difference.
func diffValues(path string, held, target any) (diffs []string) {
	switch heldValue := held.(type) {
	case map[string]any:
		if targetValue, cast := target.(map[string]any); cast {
			return diffMaps(path, heldValue, targetValue)
		}
	case []any:
		if targetValue, cast := target.([]any); cast {
			return diffLists(path, heldValue, targetValue)
		}
	}
	if fmt.Sprint(held) == fmt.Sprint(target) {
		return
	}
	diffs = append(diffs, fmt.Sprintf(
		"%s: %v in the bundle's expansion and %v in the target's", path, held, target))
	return
}

// diffLists compares two lists entry by entry, so that a difference names
// the entry it is in. It reports the two lengths when they differ.
func diffLists(path string, held, target []any) (diffs []string) {
	for i := 0; i < len(held) && i < len(target); i++ {
		diffs = append(diffs, diffValues(fmt.Sprintf("%s[%d]", path, i), held[i], target[i])...)
	}
	if len(held) == len(target) {
		return
	}
	diffs = append(diffs, fmt.Sprintf(
		"%s: %d entries in the bundle's expansion and %d in the target's",
		path,
		len(held),
		len(target)))
	return
}

// TestDeployingTheCapturedBundleReproducesTheVirtualMachine is the question
// phase 1 left open: does a bundle actually reproduce the VirtualMachine?
//
// The tests share one capture. The test deploys it into a second namespace
// on the same cluster. The deploy goes through the library entry point with
// the real cluster adapter. That is the first time anything drives a deploy
// against an API server that can refuse it.
//
// The test then compares the target against the fixture. An object the
// fixture has that the target lacks is a failure, named. The test also
// diffs the target's own expansion of the machine against the expansion
// the bundle holds. It reports the differences, because the two clusters
// resolving differently is information.
//
// Along the way the round trip settles three facts the design depends on.
// Does the expansion carry the instancetype and preference matchers (the
// applied machine names none whatever it carries)? Do the
// dataVolumeTemplates survive it (the applied machine carries none, and the
// volume references that named them are satisfied unchanged by the
// standalone DataVolumes)? Does CDI's upload accept the bundle's compressed
// bytes unchanged (the deployed disk arrives populated)?
//
// The round trip does not exercise the tar.gz form. The tar.gz the export
// produces of a filesystem volume carries the volume's own root entry. A
// CDI running under OpenShift's restricted security context cannot untar
// that into the volume it lands in. So the gzip form is what the round
// trip covers. The deploy suite's disk fixtures cover the tar.gz bytes'
// pass-through.
//
// Everything the round trip creates is deleted again however it ends. The
// undeploy at the end deletes the deployment by its label. The second
// namespace is deleted after that, which is the sure end.
func TestDeployingTheCapturedBundleReproducesTheVirtualMachine(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	s := live(t)
	_, manifest := s.captured(t)
	ctx := context.Background()

	target := s.targetNamespace(t, ctx)
	opt := deploy.Options{
		Bundle:    s.capture.path,
		Namespace: target,
	}
	if s.fixture.attached != "" {
		// The definition the machine's secondary network names lives in
		// the fixture's namespace. So the deploy namespace holds nothing
		// of that name for the network to map onto. The mapping names
		// where it does live. It is qualified because that is outside
		// the deploy namespace.
		opt.Networks = map[string]string{
			networkSecondary: s.fixture.namespace + "/" + s.fixture.attached,
		}
	}
	record, err := deploy.Deploy(ctx, s.quo, opt)
	// A partial deploy is a failed round trip. The steps are checked first.
	// So the failure names the object that did not arrive and why.
	for i := range record.Steps {
		g.Expect(record.Steps[i].Outcome).To(gomega.Equal(deploy.OutcomeCreated),
			stepOf(record.Steps[i]))
	}
	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Every requirement the bundle declared is reported with what it was
	// mapped onto, because nothing maps silently.
	for i := range record.Requirements {
		requirement := record.Requirements[i]
		t.Logf("%s %q -> %q (%s)", requirement.Kind, requirement.From, requirement.To, requirement.How)
	}

	// What the fixture has, the target holds. The reads go through the
	// suite's own client. So what checks quo never goes through quo.
	expected := deployedSet(manifest, target)
	missing, err := s.missing(ctx, target, expected)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(missing).To(gomega.BeEmpty(),
		"every object the fixture has of a kind a deploy creates should be on the target")

	// The disks arrived populated. CDI's upload is what put the bundle's
	// bytes on the target, streamed unchanged in the gzip or tar.gz form
	// the export produced. The import finishing is what says the upload
	// was accepted. Both ends being CDI is the design's bet, and this is
	// where it is settled. What the target holds for a disk is asserted
	// rather than read back. The claim is bound, at least as large as the
	// size the capture recorded, in the volume mode the capture recorded.
	for i := range manifest.Disks {
		disk := manifest.Disks[i]
		_, err = s.await(
			ctx,
			dataVolumeResource,
			target,
			disk.Claim,
			"become populated",
			provisionTimeout,
			provisioned)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		required := requirementOf(manifest, disk.Claim)
		// Which form went round is said because the fixture produces one.
		// It is whichever the export server offered the claim. The other
		// is covered by the deploy suite's disk fixtures.
		t.Logf("the disk %q went round as %s", disk.Claim, disk.Format)
		claim, readErr := s.writerFor(claimResource, target).Get(ctx, disk.Claim, meta.GetOptions{})
		g.Expect(readErr).ToNot(gomega.HaveOccurred())
		phase, _, _ := unstructured.NestedString(claim.Object, "status", "phase")
		g.Expect(phase).To(gomega.Equal("Bound"),
			fmt.Sprintf("the claim behind the deployed disk %q", disk.Claim))
		bound, _, _ := unstructured.NestedString(claim.Object, "status", "capacity", "storage")
		g.Expect(atLeast(bound, required.Size)).To(gomega.BeTrue(),
			fmt.Sprintf(
				"the claim behind the deployed disk %q is bound at %s and the bundle recorded %s",
				disk.Claim,
				bound,
				required.Size))
		mode, _, _ := unstructured.NestedString(claim.Object, "spec", "volumeMode")
		g.Expect(mode).To(gomega.Equal(required.VolumeMode),
			fmt.Sprintf("the claim behind the deployed disk %q", disk.Claim))
	}

	// The test waits for the machine the target holds until the cluster
	// accepts it as a stopped VirtualMachine. Then the test reads the
	// machine back once.
	vmName := manifest.Provenance.VirtualMachine
	deployed, err := s.await(
		ctx,
		virtualMachineResource,
		target,
		vmName,
		"settle",
		settleTimeout,
		stopped)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	// The applied machine names no instancetype or preference matcher,
	// whatever the expansion carried. The resolution is what lands. So no
	// ControllerRevision is created on the target, and none is needed.
	_, matched, _ := unstructured.NestedMap(deployed.Object, "spec", "instancetype")
	g.Expect(matched).To(gomega.BeFalse(),
		"the deployed VirtualMachine names an instancetype matcher")
	_, matched, _ = unstructured.NestedMap(deployed.Object, "spec", "preference")
	g.Expect(matched).To(gomega.BeFalse(),
		"the deployed VirtualMachine names a preference matcher")

	// The applied machine carries no dataVolumeTemplates. They became
	// standalone DataVolumes ahead of it. The volume stanzas that named
	// them are the expansion's own, satisfied unchanged.
	_, declared, _ := unstructured.NestedSlice(deployed.Object, "spec", "dataVolumeTemplates")
	g.Expect(declared).To(gomega.BeFalse(),
		"the deployed VirtualMachine declares dataVolumeTemplates")
	heldExpansion := s.read(t, bundle.ExpandedSpecPath)
	volumes, _, _ := unstructured.NestedSlice(deployed.Object, "spec", "template", "spec", "volumes")
	declaredVolumes, _, _ := unstructured.NestedSlice(
		heldExpansion.Object, "spec", "template", "spec", "volumes")
	g.Expect(volumes).To(gomega.Equal(declaredVolumes),
		"the volume stanzas of the deployed VirtualMachine are the expansion's own")
	// Every volume naming a claim finds it bound.
	for i := range volumes {
		volume, cast := volumes[i].(map[string]any)
		if !cast {
			continue
		}
		claim := claimBehind(volume)
		if claim == "" {
			continue
		}
		g.Expect(s.claimBound(ctx, target, claim)).To(gomega.BeTrue(),
			fmt.Sprintf("the claim behind the deployed VirtualMachine's volume %q", claim))
	}

	// The identity the capture carried through arrives. Any of these could
	// be the thing causing the problem the bundle was captured for.
	uuid, _, _ := unstructured.NestedString(
		deployed.Object, "spec", "template", "spec", "domain", "firmware", "uuid")
	g.Expect(uuid).To(gomega.Equal(fixtureUUID))
	serial, _, _ := unstructured.NestedString(
		deployed.Object, "spec", "template", "spec", "domain", "firmware", "serial")
	g.Expect(serial).To(gomega.Equal(fixtureSerial))

	// What the expansion carried of the matchers and the templates is
	// settled by observation and reported. The assertions above hold
	// either way.
	reportExpansion(t, heldExpansion)

	// The test diffs the target's own expansion of the deployed machine
	// against the bundle's. The diff covers the spec, which is what the
	// two clusters resolved the machine to. It leaves the metadata and
	// the status alone. The deploy rewrites the metadata, and each
	// cluster keeps its own status. Neither of those is a resolution.
	// Differences are reported rather than failed, because the two
	// clusters resolving differently is information.
	expanded, err := s.quo.ExpandSpec(ctx, target, vmName)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	heldSpec, _, _ := unstructured.NestedMap(heldExpansion.Object, "spec")
	targetSpec, _, _ := unstructured.NestedMap(expanded.Object, "spec")
	diffs := differences(heldSpec, targetSpec)
	if len(diffs) == 0 {
		t.Log("the target resolves the deployed VirtualMachine exactly as the source cluster did")
	}
	for _, diff := range diffs {
		t.Log(diff)
	}

	// The undeploy deletes the deployment by its label. That is the first
	// time anything has selected one against a real API server. What the
	// undeploy deleted is exactly what the deploy put there. The undeploy
	// deleted nothing else in the namespace.
	deleted, err := deploy.Undeploy(ctx, s.quo, deploy.UndeployOptions{
		Bundle:    s.capture.path,
		Namespace: target,
	})
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(deletions(deleted)).To(gomega.ConsistOf(described(expected)))

	// CDI owns the claim behind a DataVolume through an owner reference,
	// and deletes it with the DataVolume. The round trip waits to see
	// whether the cluster did, and says so either way. The namespace
	// deletion behind the test is the sure end whatever happened.
	for i := range manifest.Disks {
		claim := manifest.Disks[i].Claim
		if s.awaitGone(ctx, claimResource, target, claim, claimGoneTimeout) {
			t.Logf("the claim %q went with the DataVolume that owned it", claim)
			continue
		}
		t.Logf("the claim %q is still there after its DataVolume went", claim)
	}
}

// targetNamespace creates the namespace the round trip deploys into. It
// deletes the namespace again however the test ends.
//
// A deploy refuses a namespace that is not there. Deploying into the
// fixture's own would collide with the fixture's objects. So the round trip
// needs a second one. Deleting the namespace deletes everything in it,
// which is the sure end whether the undeploy ran or not.
func (r *suite) targetNamespace(t *testing.T, ctx context.Context) (namespace string) {
	t.Helper()
	namespace = r.fixture.named(partTarget)
	_, err := r.writerFor(namespaceResource, "").Create(
		ctx,
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata":   map[string]any{"name": namespace},
		}},
		meta.CreateOptions{})
	if k8serr.IsForbidden(err) {
		// A kubeconfig that may not create a namespace cannot host the
		// round trip. That says nothing about quo. The capture side of
		// the suite runs with an existing namespace, and this one cannot.
		t.Skipf(
			"the round trip needs a namespace of its own to deploy into, and this kubeconfig may not create one: %v",
			err)
	}
	if err != nil {
		t.Fatalf("create the round trip's target namespace %q: %v", namespace, err)
	}
	t.Cleanup(func() {
		// The context is detached from the test's, because a test that
		// was cancelled still has a namespace to delete.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteTimeout)
		defer cancel()
		err := r.writerFor(namespaceResource, "").Delete(ctx, namespace, meta.DeleteOptions{})
		if err != nil && !k8serr.IsNotFound(err) {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"live suite: delete the round trip's namespace %q: %v\n",
				namespace,
				err)
		}
	})
	return
}

// missing names every object the given namespace does not hold of the ones
// given. The reads go through the suite's own client. So what checks quo
// never goes through quo.
func (r *suite) missing(
	ctx context.Context,
	namespace string,
	refs []cluster.ObjectRef) (missing []cluster.ObjectRef, err error) {
	//
	for i := range refs {
		_, err = r.writerFor(gvrByKind[refs[i].Kind], namespace).Get(ctx, refs[i].Name, meta.GetOptions{})
		if err == nil {
			continue
		}
		if !k8serr.IsNotFound(err) {
			err = fmt.Errorf("read %s %s/%s: %w", refs[i].Kind, namespace, refs[i].Name, err)
			return
		}
		missing = append(missing, refs[i])
	}
	err = nil
	return
}

// awaitGone reads one object back until the cluster no longer holds it. It
// reports whether the object went within the timeout.
func (r *suite) awaitGone(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	namespace, name string,
	timeout time.Duration) (gone bool) {
	//
	deadline := time.Now().Add(timeout)
	for {
		_, err := r.dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, meta.GetOptions{})
		if k8serr.IsNotFound(err) {
			gone = true
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// claimBound says whether the namespace holds the named claim and the
// cluster has bound it.
func (r *suite) claimBound(ctx context.Context, namespace, claim string) (bound bool) {
	held, err := r.writerFor(claimResource, namespace).Get(ctx, claim, meta.GetOptions{})
	if err != nil {
		return
	}
	phase, _, _ := unstructured.NestedString(held.Object, "status", "phase")
	bound = phase == "Bound"
	return
}

// stopped reports a VirtualMachine that the target cluster has taken and
// left stopped. The printable status is KubeVirt's own account of a machine
// it is not running. That is what a deployed machine is meant to be.
func stopped(vm *unstructured.Unstructured) (done bool) {
	status, _, _ := unstructured.NestedString(vm.Object, "status", "printableStatus")
	done = status == capture.StatusStopped
	return
}

// reportExpansion says what the bundle's expansion carries of the fields
// whose survival the design left as open questions. Those fields are the
// instancetype and preference matchers, and the dataVolumeTemplates. The
// applied machine names none of them however the expansion reads. So the
// answer is reported rather than asserted.
func reportExpansion(t *testing.T, held *unstructured.Unstructured) {
	t.Helper()
	for _, matcher := range []string{"instancetype", "preference"} {
		if _, found, _ := unstructured.NestedMap(held.Object, "spec", matcher); found {
			t.Logf("the expansion carries the %s matcher it was asked for", matcher)
			continue
		}
		t.Logf("the expansion carries no %s matcher", matcher)
	}
	templates, found, _ := unstructured.NestedSlice(held.Object, "spec", "dataVolumeTemplates")
	if !found {
		t.Log("the expansion carries no dataVolumeTemplates")
		return
	}
	t.Logf("the expansion carries %d dataVolumeTemplates, which the applied VirtualMachine does not", len(templates))
}

// requirementOf is the storage requirement the bundle records for one claim.
func requirementOf(manifest *bundle.Manifest, claim string) (required bundle.StorageRequirement) {
	for i := range manifest.Requirements.Storage {
		if manifest.Requirements.Storage[i].Claim != claim {
			continue
		}
		required = manifest.Requirements.Storage[i]
		return
	}
	return
}

// claimBehind is the claim one of the machine's volumes names, empty for a
// volume backed by something other than storage. A DataVolume provisions a
// claim of its own name. So both forms resolve to a claim.
func claimBehind(volume map[string]any) (claim string) {
	claim, _, _ = unstructured.NestedString(volume, "persistentVolumeClaim", "claimName")
	if claim != "" {
		return
	}
	claim, _, _ = unstructured.NestedString(volume, "dataVolume", "name")
	return
}

// atLeast says whether one quantity is another or more, false when either
// does not parse as one.
func atLeast(size, least string) (enough bool) {
	held, err := resource.ParseQuantity(size)
	if err != nil {
		return
	}
	recorded, err := resource.ParseQuantity(least)
	if err != nil {
		return
	}
	enough = held.Cmp(recorded) >= 0
	return
}

// described names every ref the way a failure or a report reads one.
func described(refs []cluster.ObjectRef) (names []string) {
	for i := range refs {
		names = append(names, describe(refs[i]))
	}
	return
}

// deletions names what an undeploy deleted, the same way.
func deletions(deleted []deploy.Deletion) (names []string) {
	for i := range deleted {
		names = append(names, describe(cluster.ObjectRef{
			Kind:      deleted[i].Kind,
			Namespace: deleted[i].Namespace,
			Name:      deleted[i].Name,
		}))
	}
	return
}

// stepOf says what became of one object a deploy attempted, for the failure
// that names it.
func stepOf(step deploy.Step) (said string) {
	said = fmt.Sprintf(
		"the deploy left the %s %s/%s %s: %s",
		step.Kind,
		step.Namespace,
		step.Name,
		step.Outcome,
		step.Reason)
	return
}
