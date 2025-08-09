package grpcserver

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/aaronjoju07/localcloud-backend-go/internal/auth"
	"github.com/aaronjoju07/localcloud-backend-go/internal/storage"
	"github.com/aaronjoju07/localcloud-backend-go/proto"
)

func RegisterServices(s *grpc.Server, pool *pgx.Conn) {
	// Implementation note: pool currently not used directly here; adapt to pgxpool if needed.
	st := storage.New(getenv("STORAGE_INTERNAL_ROOT"), getenv("STORAGE_EXTERNAL_ROOT"))
	proto.RegisterAuthServiceServer(s, &authServer{pool: pool})
	proto.RegisterFileServiceServer(s, &fileServer{pool: pool, storage: st})
}

func getenv(key string) string {
	v := strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(""), "\n", ""))
	if v = strings.TrimSpace(v); v == "" {
		return ""
	}
	return v
}

// auth server
type authServer struct {
	proto.UnimplementedAuthServiceServer
	pool *pgx.Conn
}

func (a *authServer) Register(ctx context.Context, req *proto.RegisterRequest) (*proto.AuthResponse, error) {
	// Insert user into DB with bcrypt password - simplified here
	id := uuid.New().String()
	// TODO: hash password
	_, err := a.pool.Exec(ctx, "INSERT INTO users(id, username, password_hash, role) VALUES($1,$2,$3,$4)", id, req.Username, req.Password, req.Role)
	if err != nil {
		return nil, err
	}
	token, _ := auth.GenerateToken(id, req.Role, 24*3600*1)
	return &proto.AuthResponse{Token: token}, nil
}

func (a *authServer) Login(ctx context.Context, req *proto.LoginRequest) (*proto.AuthResponse, error) {
	// TODO: verify password
	row := a.pool.QueryRow(ctx, "SELECT id, role, password_hash FROM users WHERE username=$1", req.Username)
	var id string
	var role string
	var pw string
	if err := row.Scan(&id, &role, &pw); err != nil {
		return nil, err
	}
	// skipping password verify in example
	token, _ := auth.GenerateToken(id, role, 24*3600*1)
	return &proto.AuthResponse{Token: token}, nil
}

// file server
type fileServer struct {
	proto.UnimplementedFileServiceServer
	pool    *pgx.Conn
	storage *storage.Storage
}

func (f *fileServer) ListFiles(ctx context.Context, req *proto.ListFilesRequest) (*proto.ListFilesResponse, error) {
	// Extract user from metadata
	md, _ := metadata.FromIncomingContext(ctx)
	var userID, role string
	if vals := md.Get("authorization"); len(vals) > 0 {
		claims, err := auth.ParseToken(strings.TrimPrefix(vals[0], "Bearer "))
		if err == nil {
			userID = fmt.Sprintf("%v", claims["sub"])
			role = fmt.Sprintf("%v", claims["role"])
		}
	}

	// If user role is user, list only that user's files
	if role != "admin" {
		// query DB for files of this user & storage type + path prefix
		rows, err := f.pool.Query(ctx, "SELECT id, user_id, storage_type, relative_path, size, created_at FROM files WHERE user_id=$1 AND storage_type=$2 AND relative_path LIKE $3", userID, req.StorageType, req.Path+"%")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		resp := &proto.ListFilesResponse{}
		for rows.Next() {
			var fm proto.FileMeta
			var created pgx.NullTime
			if err := rows.Scan(&fm.Id, &fm.UserId, &fm.StorageType, &fm.RelativePath, &fm.Size, &created); err != nil {
				return nil, err
			}
			resp.Files = append(resp.Files, &fm)
		}
		return resp, nil
	}

	// admin: list all
	rows, err := f.pool.Query(ctx, "SELECT id, user_id, storage_type, relative_path, size, created_at FROM files WHERE storage_type=$1 AND relative_path LIKE $2", req.StorageType, req.Path+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	resp := &proto.ListFilesResponse{}
	for rows.Next() {
		var fm proto.FileMeta
		var created pgx.NullTime
		if err := rows.Scan(&fm.Id, &fm.UserId, &fm.StorageType, &fm.RelativePath, &fm.Size, &created); err != nil {
			return nil, err
		}
		resp.Files = append(resp.Files, &fm)
	}
	return resp, nil
}

func (f *fileServer) UploadFile(stream proto.FileService_UploadFileServer) (*proto.UploadResponse, error) {
	// receive first meta message & then chunks
	var meta *proto.FileMeta
	buf := new(strings.Builder)
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if req.Meta != nil {
			meta = req.Meta
			continue
		}
		if req.ChunkData != nil {
			buf.Write(req.ChunkData)
		}
	}
	if meta == nil {
		return nil, fmt.Errorf("missing meta")
	}

	// store via storage.SaveStream
	reader := strings.NewReader(buf.String())
	storedRel, size, err := f.storage.SaveStream(stream.Context(), meta.StorageType, meta.UserId, meta.RelativePath, reader)
	if err != nil {
		return nil, err
	}

	// insert DB record
	_, err = f.pool.Exec(stream.Context(), "INSERT INTO files(id, user_id, storage_type, relative_path, size) VALUES($1,$2,$3,$4,$5)", meta.Id, meta.UserId, meta.StorageType, storedRel, size)
	if err != nil {
		return nil, err
	}

	return &proto.UploadResponse{FileId: meta.Id}, nil
}

func (f *fileServer) DownloadFile(req *proto.DownloadRequest, stream proto.FileService_DownloadFileServer) error {
	// fetch DB entry
	row := f.pool.QueryRow(stream.Context(), "SELECT storage_type, relative_path FROM files WHERE id=$1", req.FileId)
	var storageType, relPath string
	if err := row.Scan(&storageType, &relPath); err != nil {
		return err
	}

	fhandle, err := f.storage.OpenForRead(stream.Context(), storageType, relPath)
	if err != nil {
		return err
	}
	defer fhandle.Close()

	buf := make([]byte, 64*1024)
	for {
		n, err := fhandle.Read(buf)
		if n > 0 {
			if err := stream.Send(&proto.DownloadChunk{ChunkData: buf[:n]}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}
