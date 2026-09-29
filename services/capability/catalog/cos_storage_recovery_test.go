package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// fakeCOS emulates the COS object API subset the Storage Provider uses: multipart
// staging, shard listing, server-side copy to the content-addressed immutable
// key, and object head/get/delete. It exists so the crash-recovery and
// reconciliation behaviour is proven against real SDK request/response shapes
// without a real bucket or credential.
type fakeCOS struct {
	mu      sync.Mutex
	nextID  int
	objects map[string][]byte
	uploads map[string]map[int][]byte
	parts   map[string]map[int]string
}

func newFakeCOS() *fakeCOS {
	return &fakeCOS{objects: map[string][]byte{}, uploads: map[string]map[int][]byte{}, parts: map[string]map[int]string{}}
}

func writeCOSXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func writeCOSAbsent(w http.ResponseWriter) {
	writeCOSXML(w, http.StatusNotFound, `<Error><Code>NoSuchKey</Code><Message>absent</Message></Error>`)
}

func (f *fakeCOS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/")
	q := r.URL.Query()
	uploadID := q.Get("uploadId")
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		f.nextID++
		id := "upload-" + strconv.Itoa(f.nextID)
		f.uploads[id] = map[int][]byte{}
		f.parts[id] = map[int]string{}
		writeCOSXML(w, http.StatusOK, fmt.Sprintf(`<InitiateMultipartUploadResult><Bucket>opl-test</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, key, id))
	case r.Method == http.MethodPut && q.Get("partNumber") != "":
		parts, ok := f.uploads[uploadID]
		if !ok {
			writeCOSAbsent(w)
			return
		}
		number, err := strconv.Atoi(q.Get("partNumber"))
		if err != nil {
			writeCOSXML(w, http.StatusBadRequest, `<Error><Code>InvalidArgument</Code></Error>`)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeCOSXML(w, http.StatusInternalServerError, `<Error><Code>InternalError</Code></Error>`)
			return
		}
		parts[number] = body
		etag := fmt.Sprintf(`"etag-%s-%d"`, uploadID, number)
		f.parts[uploadID][number] = etag
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && uploadID != "":
		parts, ok := f.uploads[uploadID]
		if !ok {
			writeCOSAbsent(w)
			return
		}
		numbers := make([]int, 0, len(parts))
		for number := range parts {
			numbers = append(numbers, number)
		}
		sort.Ints(numbers)
		var out strings.Builder
		fmt.Fprintf(&out, `<ListPartsResult><Bucket>opl-test</Bucket><Key>%s</Key><UploadId>%s</UploadId><IsTruncated>false</IsTruncated>`, key, uploadID)
		for _, number := range numbers {
			fmt.Fprintf(&out, `<Part><PartNumber>%d</PartNumber><ETag>%s</ETag><Size>%d</Size></Part>`, number, f.parts[uploadID][number], len(parts[number]))
		}
		out.WriteString(`</ListPartsResult>`)
		writeCOSXML(w, http.StatusOK, out.String())
	case r.Method == http.MethodPost && uploadID != "":
		parts, ok := f.uploads[uploadID]
		if !ok {
			writeCOSAbsent(w)
			return
		}
		numbers := make([]int, 0, len(parts))
		for number := range parts {
			numbers = append(numbers, number)
		}
		sort.Ints(numbers)
		var assembled []byte
		for _, number := range numbers {
			assembled = append(assembled, parts[number]...)
		}
		f.objects[key] = assembled
		delete(f.uploads, uploadID)
		delete(f.parts, uploadID)
		writeCOSXML(w, http.StatusOK, fmt.Sprintf(`<CompleteMultipartUploadResult><Bucket>opl-test</Bucket><Key>%s</Key><ETag>"whole"</ETag></CompleteMultipartUploadResult>`, key))
	case r.Method == http.MethodDelete && uploadID != "":
		delete(f.uploads, uploadID)
		delete(f.parts, uploadID)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && r.Header.Get("x-cos-copy-source") != "":
		source := strings.SplitN(r.Header.Get("x-cos-copy-source"), "/", 2)
		if len(source) != 2 {
			writeCOSXML(w, http.StatusBadRequest, `<Error><Code>InvalidArgument</Code></Error>`)
			return
		}
		body, ok := f.objects[source[1]]
		if !ok {
			writeCOSAbsent(w)
			return
		}
		f.objects[key] = body
		writeCOSXML(w, http.StatusOK, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodHead:
		if _, ok := f.objects[key]; !ok {
			writeCOSAbsent(w)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(f.objects[key])))
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet:
		body, ok := f.objects[key]
		if !ok {
			writeCOSAbsent(w)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	default:
		writeCOSXML(w, http.StatusBadRequest, `<Error><Code>InvalidRequest</Code></Error>`)
	}
}

// fakeCOSStorage binds the production COS provider to a fake bucket so the
// provider's own request/response handling is exercised, including the SDK's
// signing transport.
func fakeCOSStorage(t *testing.T, server *httptest.Server) *cosStorage {
	t.Helper()
	bucketURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{SecretID: "AKIDEXAMPLE", SecretKey: "SECRETEXAMPLE", Transport: server.Client().Transport},
	})
	store := &cosStorage{client: client, cfg: COSConfig{Bucket: "opl-test", Region: "na-siliconvalley", SecretID: "AKIDEXAMPLE", SecretKey: "SECRETEXAMPLE"}}
	if err := store.Ready(context.Background()); err != nil {
		t.Fatalf("fake bucket must be reachable: %v", err)
	}
	return store
}

// fakeCOSParts writes one shard per entry into the provider staging key the
// Capability upload id addresses, bound to the provider upload session id.
func fakeCOSParts(t *testing.T, server *httptest.Server, upload, providerUploadRef string, parts map[int]string) {
	t.Helper()
	for number, body := range parts {
		req, err := http.NewRequest(http.MethodPut, server.URL+"/staging/"+upload+"?partNumber="+strconv.Itoa(number)+"&uploadId="+providerUploadRef, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.ContentLength = int64(len(body))
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("fake part upload status=%d", resp.StatusCode)
		}
	}
}

// TestCOSAssembleRecoversCopiedImmutableObjectAfterCrash proves the recovery
// window is closed: after the archive-validated bytes were copied to their
// content-addressed immutable key and the staging artifact was cleaned, but
// before the owner transaction committed, a retry of the same Complete
// operation must finalize from the immutable object instead of failing or
// re-uploading.
func TestCOSAssembleRecoversCopiedImmutableObjectAfterCrash(t *testing.T) {
	fake := newFakeCOS()
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	store := fakeCOSStorage(t, server)
	ctx := context.Background()

	upload := "upload-crash"
	first, second := "first shard bytes", "second shard bytes"
	total := []byte(first + second)
	want := digest(total)
	ref, err := store.BeginUpload(ctx, upload, want, int64(len(total)))
	if err != nil {
		t.Fatal(err)
	}
	fakeCOSParts(t, server, upload, ref, map[int]string{1: first, 2: second})
	confirmed := []ConfirmedPart{{PartNumber: 1, SizeBytes: int64(len(first)), Sha256: digest([]byte(first))}, {PartNumber: 2, SizeBytes: int64(len(second)), Sha256: digest([]byte(second))}}
	assembled, err := store.Assemble(ctx, upload, ref, want, int64(len(total)), confirmed)
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, len(total))
	if _, err = assembled.File.ReadAt(body, 0); err != nil {
		t.Fatal(err)
	}
	if string(body) != string(total) {
		t.Fatalf("assembled bytes=%q", body)
	}

	// The owner copies the validated bytes to their content-addressed key. The
	// crash: the multipart is gone, staging is cleaned up, and the owner never
	// committed. The retry must succeed from the immutable object.
	if err = store.Promote(ctx, upload, want, assembled); err != nil {
		t.Fatal(err)
	}
	assembled.Cleanup()
	fake.mu.Lock()
	delete(fake.objects, stagingKey(upload))
	fake.mu.Unlock()
	view, err := store.ListParts(ctx, upload, ref, want)
	if err != nil || !view.Complete {
		t.Fatalf("finalized upload view=%+v err=%v", view, err)
	}
	retried, err := store.Assemble(ctx, upload, ref, want, int64(len(total)), confirmed)
	if err != nil {
		t.Fatalf("crash retry must recover the copied artifact: %v", err)
	}
	defer retried.Cleanup()
	again := make([]byte, len(total))
	if _, err = retried.File.ReadAt(again, 0); err != nil {
		t.Fatal(err)
	}
	if string(again) != string(total) {
		t.Fatalf("recovered bytes=%q", again)
	}
}

// TestCOSRecoveryReVerifiesTheImmutableObject pins that the recovery path
// re-verifies the stored bytes instead of trusting the key: after the validated
// bytes were copied to their content-addressed key (Promote) and the staging
// artifact was cleaned, an inconsistent object under that key is reported as a
// digest mismatch, and a missing finalized upload is reported unavailable rather
// than as absence.
func TestCOSRecoveryReVerifiesTheImmutableObject(t *testing.T) {
	fake := newFakeCOS()
	server := httptest.NewTLSServer(fake)
	defer server.Close()
	store := fakeCOSStorage(t, server)
	ctx := context.Background()

	upload := "upload-corrupt"
	body := "real bytes"
	want := digest([]byte(body))
	ref, err := store.BeginUpload(ctx, upload, want, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	fakeCOSParts(t, server, upload, ref, map[int]string{1: body})
	confirmed := []ConfirmedPart{{PartNumber: 1, SizeBytes: int64(len(body)), Sha256: want}}
	assembled, err := store.Assemble(ctx, upload, ref, want, int64(len(body)), confirmed)
	if err != nil {
		t.Fatal(err)
	}
	// The owner copies the archive-validated bytes to their content-addressed
	// key; the crash window opens here, before the owner transaction commits,
	// and the staging artifact is cleaned. A retry must recover from the
	// immutable object.
	key, err := immutableKey(want)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Promote(ctx, upload, want, assembled); err != nil {
		t.Fatal(err)
	}
	assembled.Cleanup()
	fake.mu.Lock()
	delete(fake.objects, stagingKey(upload))
	// Simulate an inconsistent object store: the content-addressed key holds
	// bytes that do not hash to the digest it is keyed by.
	fake.objects[key] = []byte("tampered bytes")
	fake.mu.Unlock()
	if _, err = store.Assemble(ctx, upload, ref, want, int64(len(body)), confirmed); err != ErrDigestMismatch {
		t.Fatalf("tampered immutable readback error=%v", err)
	}

	// A digest whose finalized upload never existed anywhere must fail closed as
	// unavailable, not be reported as a missing object.
	other := digest([]byte("never uploaded"))
	if _, err = store.Assemble(ctx, upload, ref, other, int64(len(body)), confirmed); err != ErrStorageUnavailable {
		t.Fatalf("unknown finalized upload error=%v", err)
	}
	if _, err = store.ListParts(ctx, upload, ref, other); err != ErrStorageUnavailable {
		t.Fatalf("unknown finalized upload must not be reported present: %v", err)
	}
}

// TestCOSListPartsReconcilesMidUploadShards proves a resumed upload sees exactly
// the shards the provider already holds, so only the missing ones are re-sent.
func TestCOSListPartsReconcilesMidUploadShards(t *testing.T) {
	server := httptest.NewTLSServer(newFakeCOS())
	defer server.Close()
	store := fakeCOSStorage(t, server)
	ctx := context.Background()

	upload := "upload-resume"
	ref, err := store.BeginUpload(ctx, upload, digest([]byte("whole")), 4096)
	if err != nil {
		t.Fatal(err)
	}
	fakeCOSParts(t, server, upload, ref, map[int]string{2: "second"})
	view, err := store.ListParts(ctx, upload, ref, digest([]byte("whole")))
	if err != nil {
		t.Fatal(err)
	}
	if view.Complete {
		t.Fatal("an in-progress multipart is not complete")
	}
	if len(view.Parts) != 1 {
		t.Fatalf("provider sorted view=%+v", view)
	}
	if part, ok := view.Parts[2]; !ok || part.SizeBytes != int64(len("second")) || part.Etag == "" {
		t.Fatalf("provider part two=%+v", part)
	}
	if _, ok := view.Parts[1]; ok {
		t.Fatal("a shard the provider never received must not be reported")
	}
}
