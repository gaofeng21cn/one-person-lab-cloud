package tkeapply

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	contracts "opl-cloud/packages/contracts/go"
)

func TestExtractImageCommandSelectsExactPlatform(t *testing.T) {
	for _, test := range []struct {
		name     string
		cfg      *v1.ConfigFile
		platform string
		want     []string
		invalid  bool
	}{
		{name: "single", cfg: &v1.ConfigFile{OS: "linux", Architecture: "amd64", Config: v1.Config{Cmd: []string{"node", "scripts/headless/run.mjs"}}}, platform: "linux/amd64", want: []string{"node", "scripts/headless/run.mjs"}},
		{name: "variant", cfg: &v1.ConfigFile{OS: "linux", Architecture: "arm", Variant: "v7", Config: v1.Config{Cmd: []string{"arm"}}}, platform: "linux/arm/v7", want: []string{"arm"}},
		{name: "empty", cfg: &v1.ConfigFile{OS: "linux", Architecture: "amd64", Config: v1.Config{Cmd: nil}}, platform: "linux/amd64"},
		{name: "wrong platform", cfg: &v1.ConfigFile{OS: "linux", Architecture: "arm64", Config: v1.Config{Cmd: []string{"arm64"}}}, platform: "linux/amd64", invalid: true},
		{name: "nil config", cfg: nil, platform: "linux/amd64", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := extractImageCommand(test.cfg, test.platform)
			if test.invalid {
				if !errors.Is(err, errImageCommand) {
					t.Fatalf("got %v, %v", actual, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(actual, test.want) {
				t.Fatalf("got %v, %v; want %v", actual, err, test.want)
			}
		})
	}
}

func TestExecutorPreservesMainImageCMDAndRejectsArgumentDrift(t *testing.T) {
	cluster := newFakeCluster()
	cluster.storePVC(pvcObject())
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/usr/local/bin/opl-webui-entrypoint", "entry-arg"}
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "serve"}))
	calls := 0
	executor.ImageCommand = func(_ context.Context, image, platform string) ([]string, error) {
		calls++
		if image != input.Revision.Image || platform != input.Revision.Platform {
			t.Fatalf("unbound image read: %s %s", image, platform)
		}
		return []string{"node", "scripts/headless/run.mjs", "$(LITERAL)", "$x", "$$"}, nil
	}
	result, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement())
	if err != nil || result.Status != "ready" {
		t.Fatalf("ensure=%+v %v", result, err)
	}
	component := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[0]
	depName := ComponentResourceName(input, component.Name)
	deployment := cluster.deployment(t, depName)
	if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Command, input.Revision.Entrypoint) {
		t.Fatal("entrypoint changed")
	}
	expected := []string{"node", "scripts/headless/run.mjs", "$$(LITERAL)", "$$x", "$$$$"}
	if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Args, expected) {
		t.Fatalf("args=%v", deployment.Spec.Template.Spec.Containers[0].Args)
	}
	if calls == 0 {
		t.Fatal("image CMD was not read")
	}
	deployment.Spec.Template.Spec.Containers[0].Args = []string{"foreign-server"}
	if _, err := cluster.clientset.AppsV1().Deployments(testNamespace).Update(context.Background(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err = executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || result.Status == "ready" {
		t.Fatalf("drift accepted: %+v %v", result, err)
	}
	// A replay of the original command repairs the old command-only workload;
	// a read above did not overwrite it.
	if !reflect.DeepEqual(cluster.deployment(t, depName).Spec.Template.Spec.Containers[0].Args, []string{"foreign-server"}) {
		t.Fatal("read mutated deployment")
	}
	result, err = executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement())
	if err != nil && !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("replay=%+v %v", result, err)
	}
	corrected := cluster.deployment(t, depName)
	if !reflect.DeepEqual(corrected.Spec.Template.Spec.Containers[0].Args, expected) {
		t.Fatalf("original replay did not restore image CMD: %v", corrected.Spec.Template.Spec.Containers[0].Args)
	}
}

func TestExecutorLeavesImageDefaultsWhenEntrypointIsNotOverridden(t *testing.T) {
	executor := &Executor{ImageCommand: func(context.Context, string, string) ([]string, error) {
		t.Fatal("unnecessary image read")
		return nil, nil
	}}
	args, err := executor.mainImageArgs(context.Background(), testInput(t))
	if err != nil || args != nil {
		t.Fatalf("args=%v err=%v", args, err)
	}
}

func TestImageCommandFailurePreventsApply(t *testing.T) {
	cluster := newFakeCluster()
	cluster.storePVC(pvcObject())
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "serve"}))
	executor.ImageCommand = func(context.Context, string, string) ([]string, error) { return nil, errImageCommand }
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/entrypoint"}
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); !errors.Is(err, errImageCommand) {
		t.Fatalf("err=%v", err)
	}
	if cluster.objectCount(t) != 1 {
		t.Fatal("failed image read mutated cluster")
	}
}

func TestImageCommandReadsInstallationSecretInMemoryAndCachesOnlySuccess(t *testing.T) {
	const expectedUser, expectedPass = "pull-user", "pull-pass"
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(expectedUser+":"+expectedPass))
	var failRequests atomic.Bool
	var requireAuth atomic.Bool
	var authObserved atomic.Bool

	regHandler := registry.New()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireAuth.Load() {
			if r.Header.Get("Authorization") != expectedAuth {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			authObserved.Store(true)
			if failRequests.Load() {
				http.Error(w, "https://signed.example/secret-token-url", http.StatusBadGateway)
				return
			}
		}
		regHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	imgAMD64, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		OS:           "linux",
		Architecture: "amd64",
		Config:       v1.Config{Cmd: []string{"node", "server.mjs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	imgARM64, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		OS:           "linux",
		Architecture: "arm64",
		Config:       v1.Config{Cmd: []string{"node", "arm-server.mjs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: imgAMD64, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: imgARM64, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	digest, err := idx.Digest()
	if err != nil {
		t.Fatal(err)
	}
	imageRef := host + "/opl/cmd@" + digest.String()
	ref, err := name.ParseReference(imageRef, name.StrictValidation)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.WriteIndex(ref, idx, remote.WithTransport(server.Client().Transport)); err != nil {
		t.Fatalf("seed in-memory registry: %v", err)
	}
	requireAuth.Store(true)

	key := imageRef + "\x00linux/amd64"
	imageCommandCache.Delete(key)
	t.Cleanup(func() { imageCommandCache.Delete(key) })

	dockerConfig := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, host, base64.StdEncoding.EncodeToString([]byte(expectedUser+":"+expectedPass)))
	cluster := newFakeCluster()
	cluster.storeSecret(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tcr-pull-secret", Namespace: testNamespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(dockerConfig)},
	})
	executor := &Executor{
		Client:            cluster,
		Installation:      testInstallation(),
		RegistryTransport: server.Client().Transport,
	}

	failRequests.Store(true)
	_, err = executor.readImageCommand(context.Background(), imageRef, "linux/amd64")
	if !errors.Is(err, errImageCommand) || strings.Contains(err.Error(), "secret-token-url") {
		t.Fatalf("failed registry read err=%v", err)
	}
	if _, cached := imageCommandCache.Load(key); cached {
		t.Fatal("failed read cached")
	}

	failRequests.Store(false)
	actual, err := executor.readImageCommand(context.Background(), imageRef, "linux/amd64")
	if err != nil || !reflect.DeepEqual(actual, []string{"node", "server.mjs"}) {
		t.Fatalf("args=%v err=%v", actual, err)
	}
	if !authObserved.Load() {
		t.Fatal("in-memory pull secret credentials were not sent to registry")
	}

	// Mutating returned slice must not poison cache; subsequent reads serve cache
	// without calling registry again.
	actual[0] = "changed"
	failRequests.Store(true)
	cached, err := executor.readImageCommand(context.Background(), imageRef, "linux/amd64")
	if err != nil || !reflect.DeepEqual(cached, []string{"node", "server.mjs"}) {
		t.Fatalf("cache mutated or missed: %v %v", cached, err)
	}
}

func TestImageCommandRejectsUnverifiedSecretAndMutableImage(t *testing.T) {
	for _, test := range []struct {
		name, image string
		secret      *corev1.Secret
	}{
		{name: "mutable", image: "registry.test/app:latest"},
		{name: "absent", image: "registry.test/app@sha256:" + strings.Repeat("c", 64)},
		{
			name:  "foreign namespace",
			image: "registry.test/app@sha256:" + strings.Repeat("d", 64),
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "tcr-pull-secret", Namespace: "foreign"},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{}`)},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster := newFakeCluster()
			if test.secret != nil {
				// Store under testNamespace with mismatched ObjectMeta.Namespace to
				// verify executor checks the returned object's Namespace field.
				stored := test.secret.DeepCopy()
				_, _ = cluster.clientset.CoreV1().Secrets(testNamespace).Create(context.Background(), stored, metav1.CreateOptions{})
			}
			executor := &Executor{Client: cluster, Installation: testInstallation()}
			if _, err := executor.readImageCommand(context.Background(), test.image, "linux/amd64"); !errors.Is(err, errImageCommand) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestAbsentRuntimeReadDoesNotRequireRegistry(t *testing.T) {
	cluster := newFakeCluster()
	executor := testExecutor(cluster, testInstallation())
	executor.ImageCommand = func(context.Context, string, string) ([]string, error) {
		t.Fatal("absent workload must not require registry")
		return nil, errImageCommand
	}
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/entrypoint"}
	observation, err := executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observation.Status != "absent" {
		t.Fatalf("read=%+v %v", observation, err)
	}
	lifecycle, err := executor.ReadWorkspaceApplicationRuntimeLifecycle(context.Background(), input)
	if err != nil || lifecycle.State != "absent" {
		t.Fatalf("lifecycle=%+v %v", lifecycle, err)
	}
}

type failingSecretCluster struct {
	ClusterClient
	err error
}

func (f failingSecretCluster) GetSecret(context.Context, string) (*corev1.Secret, error) {
	return nil, f.err
}

func TestImageCommandPreservesCancellationWithoutLeakingTransportError(t *testing.T) {
	executor := &Executor{
		Installation: testInstallation(),
		Client: failingSecretCluster{
			ClusterClient: newFakeCluster(),
			err:           errors.Join(context.Canceled, errors.New("sensitive transport data")),
		},
	}
	_, err := executor.readImageCommand(context.Background(), "registry.test/cancel@sha256:"+strings.Repeat("e", 64), "linux/amd64")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errImageCommand) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unsafe or lost cause: %v", err)
	}
}
