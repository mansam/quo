package deploy

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/mansam/quo/pkg/bundle"
	"github.com/mansam/quo/pkg/cluster"
)

func TestDeployAppliesTheExpandedSpecAsTheVirtualMachine(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeFixtureBundle(t, true)
	fake := target()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	// The deploy record says what the run did.
	g.Expect(record).To(gomega.Equal(Record{
		{
			Kind:      cluster.KindVirtualMachine,
			Namespace: testDeployNamespace,
			Name:      testVirtualMachineName,
			Outcome:   OutcomeCreated,
		},
	}))
	// One object is created, and only one: the bundle also holds the
	// captured declaration, an instancetype, a preference and a
	// ControllerRevision, none of which a deploy creates.
	g.Expect(fake.created).To(gomega.HaveLen(1))
	created := fake.created[0]
	g.Expect(created.GetKind()).To(gomega.Equal(cluster.KindVirtualMachine))
	g.Expect(created.GetName()).To(gomega.Equal(testVirtualMachineName))
	// What landed is the source cluster's resolution, not the declaration:
	// the machine type only the resolution knows is there, and no
	// instancetype or preference matcher is.
	machineType, found, _ := unstructured.NestedString(
		created.Object, "spec", "template", "spec", "domain", "machine", "type")
	g.Expect(found).To(gomega.BeTrue())
	g.Expect(machineType).To(gomega.Equal(testMachineType))
	_, matched, _ := unstructured.NestedMap(created.Object, "spec", "instancetype")
	g.Expect(matched).To(gomega.BeFalse())
	_, matched, _ = unstructured.NestedMap(created.Object, "spec", "preference")
	g.Expect(matched).To(gomega.BeFalse())
	// It is left stopped, as captured.
	running, found, _ := unstructured.NestedBool(created.Object, "spec", "running")
	g.Expect(found).To(gomega.BeTrue())
	g.Expect(running).To(gomega.BeFalse())
}

func TestDeployCarriesOnlyTheForcedRewrites(t *testing.T) {
	tests := []struct {
		name   string
		rename string
		want   string
	}{
		{
			name: "no name given deploys the machine under its captured name",
			want: testVirtualMachineName,
		},
		{
			name:   "a name given renames the VirtualMachine and forces no other rewrite",
			rename: "db-01-copy",
			want:   "db-01-copy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			path := writeFixtureBundle(t, true)
			opt := testOptions(path)
			opt.Name = tt.rename
			fake := target()

			record, err := Deploy(context.TODO(), fake, opt)

			g.Expect(err).ToNot(gomega.HaveOccurred())
			g.Expect(record).To(gomega.HaveLen(1))
			g.Expect(record[0].Name).To(gomega.Equal(tt.want))
			g.Expect(fake.created).To(gomega.HaveLen(1))
			// What lands is the expanded spec carrying the forced
			// rewrites — the name, the namespace, the labels, the
			// annotations, and the source cluster's metadata cleared —
			// and everything else exactly as the bundle holds it. The
			// deploy time is the one value a fixture cannot know in
			// advance, so it is taken off before the comparison and
			// asserted on its own.
			created := fake.created[0]
			annotations := created.GetAnnotations()
			deployedAt, found := annotations["quo.mansam.dev/deployed-at"]
			g.Expect(found).To(gomega.BeTrue())
			when, parseErr := time.Parse(time.RFC3339, deployedAt)
			g.Expect(parseErr).ToNot(gomega.HaveOccurred())
			g.Expect(when).To(gomega.BeTemporally("~", time.Now().UTC(), time.Minute))
			delete(annotations, "quo.mansam.dev/deployed-at")
			created.SetAnnotations(annotations)
			expected := expectedVirtualMachine(t, path, tt.want)
			g.Expect(created.Object).To(gomega.Equal(expected.Object))
		})
	}
}

func TestDeployClearsMetadataTheTargetCannotRecreate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeFixtureBundle(t, true)
	fake := target()

	_, err := Deploy(context.TODO(), fake, testOptions(path))

	// The expansion carries the metadata the source cluster assigned it,
	// and none of it is sent: the target mints its own uid, creation
	// timestamp, generation and resourceVersion when it creates, records
	// its own managed fields, and refuses outright a create that arrives
	// with a resourceVersion.
	g.Expect(err).ToNot(gomega.HaveOccurred())
	created := fake.created[0]
	for _, field := range []string{
		"resourceVersion", "uid", "creationTimestamp", "generation", "managedFields",
	} {
		_, found, _ := unstructured.NestedFieldNoCopy(created.Object, "metadata", field)
		g.Expect(found).To(gomega.BeFalse(), field)
	}
}

func TestDeployLabelsAndAnnotatesWhatItCreates(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeFixtureBundle(t, true)
	fake := target()

	_, err := Deploy(context.TODO(), fake, testOptions(path))

	g.Expect(err).ToNot(gomega.HaveOccurred())
	created := fake.created[0]
	labels := created.GetLabels()
	g.Expect(labels["app.kubernetes.io/managed-by"]).To(gomega.Equal("quo"))
	// The bundle label is the manifest's digest as a label value can hold
	// it: a hex SHA-256 is 64 characters and label values are limited to
	// 63, so the hash is base64url and prefixed, making 50.
	identity := labels["quo.mansam.dev/bundle"]
	g.Expect(identity).To(gomega.HavePrefix("sha256-"))
	g.Expect(identity).To(gomega.HaveLen(len("sha256-") + 43))
	g.Expect(identity).To(gomega.Equal(bundleLabelOf(t, path)))
	annotations := created.GetAnnotations()
	// The exact bundle filename, so a running machine can be tied back to
	// the bundle — and the case — it came from.
	g.Expect(annotations["quo.mansam.dev/bundle-file"]).To(gomega.Equal(path))
	// Where it came from and when.
	g.Expect(annotations["quo.mansam.dev/source-cluster"]).To(gomega.Equal(testSourceCluster))
	g.Expect(annotations["quo.mansam.dev/source-namespace"]).To(gomega.Equal(testSourceNamespace))
	g.Expect(annotations["quo.mansam.dev/source-virtualmachine"]).To(gomega.Equal(testVirtualMachineName))
	g.Expect(annotations["quo.mansam.dev/captured-at"]).To(
		gomega.Equal(testCapturedAt.Format(time.RFC3339)))
	// What the source cluster wrote is carried through untouched: a value
	// a general YAML library would be liable to reinterpret stays the
	// string it was.
	g.Expect(annotations[versionAnnotation]).To(gomega.Equal(versionValue))
}

func TestDeployRefusesABundleItCannotDeploy(t *testing.T) {
	path := writeFixtureBundle(t, true)
	// The same bundle with the format version rewritten, which is what a
	// bundle written by a later quo looks like. The patch keeps the length
	// so the tar the manifest indexes stays aligned.
	laterFormat := writeBytes(t, "later-format.quo", bytes.ReplaceAll(
		readFile(t, path),
		[]byte("formatVersion: "+bundle.FormatVersion),
		[]byte("formatVersion: v9")))
	// A bundle that ends inside the expansion member, which is what a
	// transfer that stopped looks like.
	content := readFile(t, path)
	expanded, err := yaml.Marshal(expandedSpec().Object)
	if err != nil {
		t.Fatalf("marshal the expanded spec: %v", err)
	}
	at := bytes.LastIndex(content, expanded)
	if at < 0 {
		t.Fatal("the fixture bundle does not carry the expanded spec")
	}
	cutShort := writeBytes(t, "cut-short.quo", content[:at+len(expanded)/2])
	// A file that is not a bundle at all, and an empty one.
	garbage := writeBytes(t, "garbage.quo", []byte("not a bundle, just bytes"))
	empty := writeBytes(t, "empty.quo", nil)

	tests := []struct {
		name   string
		bundle string
		// want is the refusal's type. A nil want means the refusal has no
		// single type worth naming and only its presence is asserted.
		want error
		// names is what the refusal names, which is how a reader tells one
		// refusal from another. Empty means the refusal names nothing a
		// test can pin down.
		names string
	}{
		{
			name:   "a bundle that is not there",
			bundle: filepath.Join(t.TempDir(), "never-written.quo"),
			names:  "never-written.quo",
		},
		{
			name:   "a file that is not a bundle",
			bundle: garbage,
		},
		{
			name:   "an empty file",
			bundle: empty,
			want:   &bundle.FormatError{},
			names:  "the bundle is empty",
		},
		{
			name:   "a bundle whose format version is unknown",
			bundle: laterFormat,
			want:   &bundle.UnsupportedVersionError{},
			names:  "v9",
		},
		{
			name:   "a bundle that ends before the expanded spec arrives",
			bundle: cutShort,
			want:   &bundle.FormatError{},
			names:  bundle.ExpandedSpecPath,
		},
		{
			name:   "a bundle that holds no expanded spec",
			bundle: writeFixtureBundle(t, false),
			want:   &NoExpandedSpecError{},
			names:  "no expanded spec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			fake := target()

			record, err := Deploy(context.TODO(), fake, testOptions(tt.bundle))

			if tt.want == nil {
				g.Expect(err).To(gomega.HaveOccurred())
			} else {
				g.Expect(err).To(gomega.MatchError(tt.want))
			}
			// The refusal names what it found.
			if tt.names != "" {
				g.Expect(err.Error()).To(gomega.ContainSubstring(tt.names))
			}
			// Nothing was attempted, so nothing was created.
			g.Expect(record).To(gomega.BeEmpty())
			g.Expect(fake.created).To(gomega.BeEmpty())
		})
	}
}

func TestDeployRefusesWhenTheTargetCannotTakeIt(t *testing.T) {
	path := writeFixtureBundle(t, true)

	tests := []struct {
		name      string
		namespace string
		taken     bool
		names     string
		want      error
	}{
		{
			name:      "a namespace the target does not have",
			namespace: "not-there",
			names:     "not-there",
			want:      &MissingNamespaceError{},
		},
		{
			name:  "a VirtualMachine already on the target",
			taken: true,
			names: testDeployNamespace + "/" + testVirtualMachineName,
			want:  &AlreadyExistsError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)
			fake := target()
			opt := testOptions(path)
			if tt.namespace != "" {
				opt.Namespace = tt.namespace
			}
			if tt.taken {
				fake.hold(cluster.ObjectRef{
					Kind:      cluster.KindVirtualMachine,
					Namespace: testDeployNamespace,
					Name:      testVirtualMachineName,
				}, declaration())
			}

			record, err := Deploy(context.TODO(), fake, opt)

			g.Expect(err).To(gomega.MatchError(tt.want))
			// The refusal names what it found.
			g.Expect(err.Error()).To(gomega.ContainSubstring(tt.names))
			g.Expect(record).To(gomega.BeEmpty())
			g.Expect(fake.created).To(gomega.BeEmpty())
		})
	}
}

func TestDeploySkipsAnExpandedSpecThatFailsItsDigest(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// One byte of the expansion changed in transit. The machine type sits
	// in the expansion alone — the declaration names an instancetype
	// instead — so its last occurrence in the bundle is in the member.
	corrupted := bytes.Clone(readFile(t, writeFixtureBundle(t, true)))
	at := bytes.LastIndex(corrupted, []byte(testMachineType))
	if at < 0 {
		t.Fatal("the fixture bundle does not carry the machine type")
	}
	corrupted[at] = 'x'
	path := writeBytes(t, "corrupted.quo", corrupted)
	fake := target()

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The step is skipped and recorded, and nothing was created:
	// corruption never reaches the target.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(record).To(gomega.HaveLen(1))
	g.Expect(record[0].Kind).To(gomega.Equal(cluster.KindVirtualMachine))
	g.Expect(record[0].Outcome).To(gomega.Equal(OutcomeSkipped))
	g.Expect(record[0].Reason).To(gomega.ContainSubstring("does not match its digest"))
	g.Expect(fake.created).To(gomega.BeEmpty())
}

func TestDeployRecordsACreateThatFailed(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	path := writeFixtureBundle(t, true)
	fake := target()
	fake.createErr = errors.New("the API server went away")

	record, err := Deploy(context.TODO(), fake, testOptions(path))

	// The run ends with what it managed and the error says how much of it
	// arrived, so that a script can tell a complete deploy from a partial
	// one.
	g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
	g.Expect(err.Error()).To(gomega.ContainSubstring("0 of the 1 objects"))
	g.Expect(record).To(gomega.HaveLen(1))
	g.Expect(record[0].Outcome).To(gomega.Equal(OutcomeFailed))
	g.Expect(record[0].Reason).To(gomega.ContainSubstring("the API server went away"))
	g.Expect(fake.created).To(gomega.BeEmpty())
}

func TestDeployStopsWhereItStandsWhenCancelled(t *testing.T) {
	path := writeFixtureBundle(t, true)

	t.Run("before anything is created", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		fake := target()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		record, err := Deploy(ctx, fake, testOptions(path))

		// The run stops, nothing is created, and everything already on the
		// target stays where it is: a deploy never rolls back.
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("context canceled"))
		g.Expect(record).To(gomega.BeEmpty())
		g.Expect(fake.created).To(gomega.BeEmpty())
	})

	t.Run("while the VirtualMachine is being created", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		fake := target()
		// What a create that was interrupted looks like: the client
		// reports the cancellation rather than the cluster's answer.
		fake.createErr = context.Canceled

		record, err := Deploy(context.TODO(), fake, testOptions(path))

		g.Expect(err).To(gomega.MatchError(&NotCreatedError{}))
		g.Expect(record).To(gomega.HaveLen(1))
		g.Expect(record[0].Outcome).To(gomega.Equal(OutcomeFailed))
		g.Expect(record[0].Reason).To(gomega.ContainSubstring("context canceled"))
		g.Expect(fake.created).To(gomega.BeEmpty())
	})
}

// expectedVirtualMachine builds what the target should hold after a deploy
// of the bundle at path: the fixture's expanded spec, decoded the way a
// deploy decodes it, carrying the forced rewrites the test's deploy
// performed. The rewrites are stated here as literal values rather than by
// calling the code under test, so this is an independent statement of the
// contract rather than a restatement of it.
//
// The deploy time is left out: it is the one value a fixture cannot know
// in advance, and the test asserts it on its own.
func expectedVirtualMachine(t *testing.T, path, name string) (expected *unstructured.Unstructured) {
	t.Helper()
	decoded := map[string]any{}
	err := yaml.Unmarshal(memberOf(t, path, bundle.ExpandedSpecPath), &decoded)
	if err != nil {
		t.Fatalf("decode the expanded spec: %v", err)
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
	expected.SetName(name)
	expected.SetNamespace(testDeployNamespace)
	expected.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "quo",
		"quo.mansam.dev/bundle":        bundleLabelOf(t, path),
	})
	annotations := expected.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["quo.mansam.dev/bundle-file"] = path
	annotations["quo.mansam.dev/source-cluster"] = testSourceCluster
	annotations["quo.mansam.dev/source-namespace"] = testSourceNamespace
	annotations["quo.mansam.dev/source-virtualmachine"] = testVirtualMachineName
	annotations["quo.mansam.dev/captured-at"] = testCapturedAt.Format(time.RFC3339)
	expected.SetAnnotations(annotations)
	return
}

// bundleLabelOf is the bundle label value a deploy derives from the bundle
// at path: the SHA-256 of the manifest member, encoded to fit a label
// value.
func bundleLabelOf(t *testing.T, path string) (value string) {
	t.Helper()
	summed := sha256.Sum256(memberOf(t, path, bundle.ManifestPath))
	value = "sha256-" + base64.RawURLEncoding.EncodeToString(summed[:])
	return
}

// memberOf returns the content of one member of the bundle at path.
func memberOf(t *testing.T, path, member string) (content []byte) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open bundle %s: %v", path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	tr := tar.NewReader(file)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("bundle %s has no member %q", path, member)
		}
		if err != nil {
			t.Fatalf("read bundle %s: %v", path, err)
		}
		if header.Name != member {
			continue
		}
		content, err = io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read member %q: %v", member, err)
		}
		return
	}
}

// readFile returns a bundle file's bytes.
func readFile(t *testing.T, path string) (content []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle %s: %v", path, err)
	}
	return
}

// writeBytes writes a file of its own, for fixtures that are broken in
// ways the bundle writer would never produce.
func writeBytes(t *testing.T, name string, content []byte) (path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), name)
	err := os.WriteFile(path, content, 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return
}
