package deploy

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

func TestDeployAppliesTheClosure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeClosureFixtureBundle(t)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The record says what the run did. It holds one DataVolume per disk
	// the bundle holds, then the closure objects in the order the bundle
	// holds them, then the VirtualMachine last.
	g.Expect(record.Steps).To(gomega.Equal([]Step{
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testRootClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testDataClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testGapClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindDataVolume,
			Namespace: testDeployNamespace,
			Name:      testThinClaim,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindConfigMap,
			Namespace: testDeployNamespace,
			Name:      testSysprepConfigMap,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindSecret,
			Namespace: testDeployNamespace,
			Name:      testCloudInitSecret,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindSecret,
			Namespace: testDeployNamespace,
			Name:      testRegistrySecret,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindServiceAccount,
			Namespace: testDeployNamespace,
			Name:      testServiceAccount,
			Outcome:   OutcomeCreated,
		},
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
			Outcome:   OutcomeCreated,
		},
	}))
	// Nine objects are created, and only those.
	g.Expect(fake.created).To(gomega.HaveLen(9))
	// Each closure object arrives as the bundle holds it, apart from the
	// forced rewrites that apply to every created object. Those are the
	// source cluster's metadata cleared, the namespace, the labels and
	// the annotations. Everything else is byte-for-byte.
	g.Expect(fake.created[4].Object).To(gomega.Equal(
		expectedClosureObject(t, path, cluster.KindConfigMap, testSysprepConfigMap).Object))
	g.Expect(fake.created[5].Object).To(gomega.Equal(
		expectedClosureObject(t, path, cluster.KindSecret, testCloudInitSecret).Object))
	g.Expect(fake.created[6].Object).To(gomega.Equal(
		expectedClosureObject(t, path, cluster.KindSecret, testRegistrySecret).Object))
	g.Expect(fake.created[7].Object).To(gomega.Equal(
		expectedClosureObject(t, path, cluster.KindServiceAccount, testServiceAccount).Object))
	// The disks arrived as they would without the closure: four uploads,
	// unchanged.
	g.Expect(fake.uploaded).To(gomega.HaveLen(4))
}

func TestDeployAppliesTheClosureBetweenTheUploadsAndTheVirtualMachine(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeClosureFixtureBundle(t)
	fake := storageTarget()

	_, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// Nothing in the namespace looks like a VirtualMachine until its
	// disks are in place. The machine's cloud-init, sysprep and access
	// credentials are present when it is examined. Every DataVolume is
	// created, every disk is uploaded, the closure objects are applied,
	// then the machine.
	g.Expect(fake.events).To(gomega.Equal([]string{
		"create DataVolume " + testDeployNamespace + "/" + testRootClaim,
		"create DataVolume " + testDeployNamespace + "/" + testDataClaim,
		"create DataVolume " + testDeployNamespace + "/" + testGapClaim,
		"create DataVolume " + testDeployNamespace + "/" + testThinClaim,
		"upload " + testDeployNamespace + "/" + testRootClaim,
		"upload " + testDeployNamespace + "/" + testDataClaim,
		"upload " + testDeployNamespace + "/" + testGapClaim,
		"upload " + testDeployNamespace + "/" + testThinClaim,
		"create ConfigMap " + testDeployNamespace + "/" + testSysprepConfigMap,
		"create Secret " + testDeployNamespace + "/" + testCloudInitSecret,
		"create Secret " + testDeployNamespace + "/" + testRegistrySecret,
		"create ServiceAccount " + testDeployNamespace + "/" + testServiceAccount,
		"create VirtualMachine " + testDeployNamespace + "/" + testVirtualMachineName,
	}))
}

func TestDeployLabelsAndAnnotatesTheClosure(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeClosureFixtureBundle(t)
	fake := storageTarget()

	_, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	for i := range fake.created {
		kind := fake.created[i].GetKind()
		if kind != cluster.KindConfigMap &&
			kind != cluster.KindSecret &&
			kind != cluster.KindServiceAccount {
			continue
		}
		// Every closure object carries the labels and annotations of every
		// created object, and its own beside them. What the source cluster
		// wrote is carried through untouched.
		labels := fake.created[i].GetLabels()
		g.Expect(labels["app.kubernetes.io/managed-by"]).To(gomega.Equal("quo"))
		g.Expect(labels["quo.mansam.dev/bundle"]).To(gomega.Equal(bundleLabelOf(t, path)))
		g.Expect(labels["app.kubernetes.io/part-of"]).To(gomega.Equal(testVirtualMachineName))
		annotations := fake.created[i].GetAnnotations()
		g.Expect(annotations["quo.mansam.dev/bundle-file"]).To(gomega.Equal(path))
		g.Expect(annotations[versionAnnotation]).To(gomega.Equal(versionValue))
	}
}

func TestDeployCreatesNothingTheBundleHoldsReadOnly(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// The bundle holds the machine's own declaration, the instancetype and
	// preference it names, and the ControllerRevision that pinned them. It
	// also holds the NetworkAttachmentDefinition behind its network and
	// the storage class its claims were provisioned from.
	path := writeClosureFixtureBundle(t)
	fake := storageTarget()

	_, err := Deploy(context.TODO(), fake, testOptions(path))

	// None of them is created. The target runs the source cluster's
	// resolution. A network is mapped rather than copied. The rest is
	// evidence about the source cluster, in the bundle to be read. Only
	// the DataVolumes, the closure objects and the VirtualMachine are
	// created.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	kinds := make([]string, 0, len(fake.created))
	for i := range fake.created {
		kinds = append(kinds, fake.created[i].GetKind())
	}
	g.Expect(kinds).To(gomega.Equal([]string{
		cluster.KindDataVolume,
		cluster.KindDataVolume,
		cluster.KindDataVolume,
		cluster.KindDataVolume,
		cluster.KindConfigMap,
		cluster.KindSecret,
		cluster.KindSecret,
		cluster.KindServiceAccount,
		cluster.KindVirtualMachine,
	}))
	// The one VirtualMachine is the source cluster's resolution of the
	// machine rather than the declaration the bundle also holds. Only the
	// resolution knows the machine type, and the declaration names an
	// instancetype.
	machine := fake.created[len(fake.created)-1]
	machineType, found, _ := unstructured.NestedString(
		machine.Object, "spec", "template", "spec", "domain", "machine", "type")
	g.Expect(found).To(gomega.BeTrue())
	g.Expect(machineType).To(gomega.Equal(testMachineType))
	_, matched, _ := unstructured.NestedMap(machine.Object, "spec", "instancetype")
	g.Expect(matched).To(gomega.BeFalse())
}

func TestDeploySkipsAClosureMemberThatFailsItsDigest(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// One byte of the sysprep answer file changed in transit. The answer's
	// stand-in content sits in the ConfigMap member alone. Replacing it
	// keeps the tar the manifest indexes aligned, while changing what the
	// digest is computed over.
	corrupted := bytes.ReplaceAll(
		readFile(t, writeClosureFixtureBundle(t)),
		[]byte(sysprepAnswer),
		[]byte(sysprepMangled))
	path := writeBytes(t, "corrupted-closure.quo", corrupted)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The corrupted member is skipped and recorded, and the run continues.
	// The closure objects behind it, the disks and the machine all
	// arrive. The error is what says the deploy was a partial one.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("8 of the 9 objects"))
	g.Expect(record.Steps).To(gomega.HaveLen(9))
	g.Expect(record.Steps[4].Kind).To(gomega.Equal(cluster.KindConfigMap))
	g.Expect(record.Steps[4].Name).To(gomega.Equal(testSysprepConfigMap))
	g.Expect(record.Steps[4].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record.Steps[4].Reason).To(gomega.ContainSubstring("does not match its digest"))
	g.Expect(record.Steps[5].Outcome).To(gomega.Equal(OutcomeCreated))
	g.Expect(record.Steps[8].Outcome).To(gomega.Equal(OutcomeCreated))
	g.Expect(fake.created).To(gomega.HaveLen(8))
	g.Expect(fake.uploaded).To(gomega.HaveLen(4))
}

func TestDeployRecordsAClosureCreateThatFailed(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeClosureFixtureBundle(t)
	fake := storageTarget()
	fake.createErrFor[cluster.ObjectRef{
		Kind:      cluster.KindSecret,
		Namespace: testDeployNamespace,
		Name:      testCloudInitSecret,
	}] = errors.New("admission webhook denied the request: secret is immutable")

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The failed create is recorded with its reason and the run continues.
	// The closure objects around it and the machine behind it all arrive.
	// The error is what says the deploy was a partial one.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("8 of the 9 objects"))
	g.Expect(record.Steps).To(gomega.HaveLen(9))
	g.Expect(record.Steps[5].Kind).To(gomega.Equal(cluster.KindSecret))
	g.Expect(record.Steps[5].Name).To(gomega.Equal(testCloudInitSecret))
	g.Expect(record.Steps[5].Outcome).To(gomega.Equal(OutcomeFailed))
	g.Expect(record.Steps[5].Reason).To(gomega.ContainSubstring("admission webhook denied the request"))
	g.Expect(record.Steps[4].Outcome).To(gomega.Equal(OutcomeCreated))
	g.Expect(record.Steps[8].Outcome).To(gomega.Equal(OutcomeCreated))
	g.Expect(fake.created).To(gomega.HaveLen(8))
}

func TestDeployAppliesNoClosureForAMachineThatCannotBeCreated(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// One byte of the expansion changed in transit. The machine type sits
	// in the expansion alone, because the declaration names an instancetype
	// instead. Its last occurrence in the bundle is therefore in the
	// member.
	corrupted := bytes.Clone(readFile(t, writeClosureFixtureBundle(t)))
	at := bytes.LastIndex(corrupted, []byte(testMachineType))
	if at < 0 {
		t.Fatal("the fixture bundle does not carry the machine type")
	}
	corrupted[at] = 'x'
	path := writeBytes(t, "corrupted-closure-expansion.quo", corrupted)
	fake := storageTarget()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The run ends after recording the VirtualMachine's skipped step. The
	// closure objects are not applied and the disks are not uploaded for a
	// machine that cannot be created. That is the same rule that refuses
	// a bundle holding no expansion at all.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(record.Steps).To(gomega.HaveLen(1))
	g.Expect(record.Steps[0].Kind).To(gomega.Equal(cluster.KindVirtualMachine))
	g.Expect(record.Steps[0].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record.Steps[0].Reason).To(gomega.ContainSubstring("does not match its digest"))
	g.Expect(fake.created).To(gomega.BeEmpty())
	g.Expect(fake.uploaded).To(gomega.BeEmpty())
}

func TestDeployRefusesWhenAnythingItWouldCreateIsAlreadyThere(t *testing.T) {
	path := writeClosureFixtureBundle(t)

	tests := []struct {
		name string
		held []cluster.ObjectRef
		// names is what the refusal names, which is how a reader sees
		// every colliding object in the one error.
		names []string
	}{
		{
			name: "a closure object already on the target",
			held: []cluster.ObjectRef{{
				Kind:      cluster.KindSecret,
				Namespace: testDeployNamespace,
				Name:      testCloudInitSecret,
			}},
			names: []string{
				"Secret " + testDeployNamespace + "/" + testCloudInitSecret,
			},
		},
		{
			name: "a DataVolume already on the target",
			held: []cluster.ObjectRef{{
				Kind:      cluster.KindDataVolume,
				Namespace: testDeployNamespace,
				Name:      testRootClaim,
			}},
			names: []string{
				"DataVolume " + testDeployNamespace + "/" + testRootClaim,
			},
		},
		{
			name: "the VirtualMachine already on the target",
			held: []cluster.ObjectRef{{
				Kind:      cluster.KindVirtualMachine,
				Namespace: testDeployNamespace,
				Name:      testVirtualMachineName,
			}},
			names: []string{
				"VirtualMachine " + testDeployNamespace + "/" + testVirtualMachineName,
			},
		},
		{
			name: "a DataVolume, a closure object and the VirtualMachine together",
			held: []cluster.ObjectRef{
				{
					Kind:      cluster.KindDataVolume,
					Namespace: testDeployNamespace,
					Name:      testRootClaim,
				},
				{
					Kind:      cluster.KindConfigMap,
					Namespace: testDeployNamespace,
					Name:      testSysprepConfigMap,
				},
				{
					Kind:      cluster.KindVirtualMachine,
					Namespace: testDeployNamespace,
					Name:      testVirtualMachineName,
				},
			},
			names: []string{
				"DataVolume " + testDeployNamespace + "/" + testRootClaim,
				"ConfigMap " + testDeployNamespace + "/" + testSysprepConfigMap,
				"VirtualMachine " + testDeployNamespace + "/" + testVirtualMachineName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			fake := storageTarget()
			for i := range tt.held {
				fake.hold(tt.held[i], occupied(tt.held[i]))
			}

			record, err := Deploy(context.TODO(), fake, testOptions(path))

			// One refusal names every colliding object, before anything
			// is created: someone else's work is neither overwritten nor
			// built around.
			g.Expect(err).To(gomega.MatchError(&AlreadyExistsError{}))
			for _, named := range tt.names {
				g.Expect(err.Error()).To(gomega.ContainSubstring(named))
			}
			g.Expect(record.Steps).To(gomega.BeEmpty())
			g.Expect(fake.created).To(gomega.BeEmpty())
			g.Expect(fake.uploaded).To(gomega.BeEmpty())
		})
	}
}

// occupied builds the object a held reference names, because only its
// presence matters to the collision check.
func occupied(ref cluster.ObjectRef) (held *unstructured.Unstructured) {
	held = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       ref.Kind,
		"metadata": map[string]any{
			"name":      ref.Name,
			"namespace": ref.Namespace,
		},
	}}
	return
}

// expectedClosureObject states what the target should hold for one of the
// closure fixtures. It is the member as the bundle holds it, decoded the
// way a deploy decodes it, carrying the forced rewrites. The rewrites are
// stated here as literal values rather than by calling the code under
// test. This is therefore an independent statement of the contract
// rather than a restatement of it.
func expectedClosureObject(
	t *testing.T,
	path, kind, name string) (expected *unstructured.Unstructured) {
	//
	t.Helper()
	decoded := map[string]any{}
	err := yaml.Unmarshal(memberOf(t, path, bundle.ResourcePath(kind, name)), &decoded)
	if err != nil {
		t.Fatalf("decode the %s %s: %v", kind, name, err)
	}
	expected = &unstructured.Unstructured{Object: decoded}
	// The metadata the source cluster assigned itself is absent: the
	// target mints its own when it creates, so none of it can be
	// recreated there.
	for _, field := range []string{
		"resourceVersion", "uid", "creationTimestamp", "generation", "managedFields",
	} {
		unstructured.RemoveNestedField(expected.Object, "metadata", field)
	}
	expected.SetNamespace(testDeployNamespace)
	// The labels and annotations quo adds sit beside the object's own.
	labels := expected.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["app.kubernetes.io/managed-by"] = "quo"
	labels["quo.mansam.dev/bundle"] = bundleLabelOf(t, path)
	expected.SetLabels(labels)
	annotations := expected.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["quo.mansam.dev/bundle-file"] = path
	expected.SetAnnotations(annotations)
	return
}
