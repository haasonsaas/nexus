package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/haasonsaas/nexus/internal/artifacts"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

type fakeArtifactRepo struct {
	artifact *proto.Artifact
	reader   io.ReadCloser
	data     []byte
	err      error
}

func (f *fakeArtifactRepo) StoreArtifact(context.Context, *proto.Artifact, io.Reader) error {
	return nil
}

func (f *fakeArtifactRepo) GetArtifact(_ context.Context, artifactID string) (*proto.Artifact, io.ReadCloser, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	artifact := f.artifact
	if artifact == nil {
		artifact = &proto.Artifact{Id: artifactID}
	}
	if f.reader != nil {
		return artifact, f.reader, nil
	}
	return artifact, io.NopCloser(bytes.NewReader(f.data)), nil
}

func (f *fakeArtifactRepo) ListArtifacts(context.Context, artifacts.Filter) ([]*proto.Artifact, error) {
	return nil, nil
}

func (f *fakeArtifactRepo) DeleteArtifact(context.Context, string) error {
	return nil
}

func (f *fakeArtifactRepo) PruneExpired(context.Context) (int, error) {
	return 0, nil
}

type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, fmt.Errorf("read not expected") }
func (errorReadCloser) Close() error             { return nil }

func TestGRPCService_GetArtifact_IncludeDataWithinLimit(t *testing.T) {
	t.Parallel()

	maxBytes := artifacts.MaxInlineDataBytes
	data := make([]byte, int(maxBytes))
	repo := &fakeArtifactRepo{
		artifact: &proto.Artifact{Id: "a1", Size: int64(len(data))},
		data:     data,
	}

	service := &grpcService{server: &Server{artifactRepo: repo}}
	resp, err := service.GetArtifact(context.Background(), &proto.GetArtifactRequest{
		ArtifactId:  "a1",
		IncludeData: true,
	})
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if resp == nil || resp.Artifact == nil {
		t.Fatalf("GetArtifact() response missing artifact")
	}
	if got := int64(len(resp.Data)); got != int64(len(data)) {
		t.Fatalf("GetArtifact() data size = %d, want %d", got, len(data))
	}
}

func TestGRPCService_GetArtifact_IncludeDataTooLarge_SizePrecheck(t *testing.T) {
	t.Parallel()

	maxBytes := artifacts.MaxInlineDataBytes
	repo := &fakeArtifactRepo{
		artifact: &proto.Artifact{Id: "a1", Size: maxBytes + 1},
		reader:   errorReadCloser{},
	}

	service := &grpcService{server: &Server{artifactRepo: repo}}
	_, err := service.GetArtifact(context.Background(), &proto.GetArtifactRequest{
		ArtifactId:  "a1",
		IncludeData: true,
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("GetArtifact() code = %v, want %v (err=%v)", status.Code(err), codes.ResourceExhausted, err)
	}
}

func TestGRPCService_GetArtifact_IncludeDataTooLarge_ReadLimit(t *testing.T) {
	t.Parallel()

	maxBytes := artifacts.MaxInlineDataBytes
	data := make([]byte, int(maxBytes)+1)
	repo := &fakeArtifactRepo{
		artifact: &proto.Artifact{Id: "a1", Size: 0},
		data:     data,
	}

	service := &grpcService{server: &Server{artifactRepo: repo}}
	_, err := service.GetArtifact(context.Background(), &proto.GetArtifactRequest{
		ArtifactId:  "a1",
		IncludeData: true,
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("GetArtifact() code = %v, want %v (err=%v)", status.Code(err), codes.ResourceExhausted, err)
	}
}
