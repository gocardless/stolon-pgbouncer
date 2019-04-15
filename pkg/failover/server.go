package failover

import (
	"context"
	"time"

	kitlog "github.com/go-kit/kit/log"
	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer"
	"github.com/golang/protobuf/ptypes"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	uuid "github.com/satori/go.uuid"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the hooks required to provide the failover interface
type Server struct {
	logger  kitlog.Logger
	bouncer *pgbouncer.PgBouncer
}

func NewServer(logger kitlog.Logger, bouncer *pgbouncer.PgBouncer) *Server {
	return &Server{
		logger:  logger,
		bouncer: bouncer,
	}
}

// LoggingInterceptor returns a UnaryServerInterceptor that logs all incoming
// requests, both at the start and at the end of their execution.
func (s *Server) LoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	logger := kitlog.With(s.logger, "method", info.FullMethod, "trace", uuid.NewV4().String())
	logger.Log("msg", "handling request")

	defer func(begin time.Time) {
		if err != nil {
			logger = kitlog.With(logger, "error", err.Error())
		}

		logger.Log("duration", time.Since(begin).Seconds())
	}(time.Now())

	return handler(ctx, req)
}

func (s *Server) HealthCheck(ctx context.Context, _ *Empty) (*HealthCheckResponse, error) {
	resp := &HealthCheckResponse{Status: HealthCheckResponse_HEALTHY}

	// TODO: Provide error in the health check response
	// https://github.com/gocardless/stolon-pgbouncer/pull/11
	if _, err := s.bouncer.ShowDatabases(ctx); err != nil {
		resp.Status = HealthCheckResponse_UNHEALTHY
	}

	return resp, nil
}

func (s *Server) Pause(ctx context.Context, req *PauseRequest) (resp *PauseResponse, err error) {
	var (
		createdAt = time.Now()
		timeout   = time.Duration(req.Timeout)
		expiry    = time.Duration(req.Expiry)
		expiresAt = createdAt.Add(expiry)
	)

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := s.bouncer.Pause(timeoutCtx); err != nil {
		if timeoutCtx.Err() == nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}

		return nil, status.Errorf(codes.DeadlineExceeded, "exceeded pause timeout")
	}

	// We need to ensure we remove the pause at expiry seconds from the moment the request
	// was received. This ensures we don't leave PgBouncer in a paused state if migration
	// goes wrong.
	if req.Expiry > 0 {
		go func() {
			s.logger.Log("msg", "scheduling pgbouncer resume", "at", iso3339(expiresAt))
			time.Sleep(time.Until(expiresAt))

			// Timeout our resume with the same timeout we gave to our pause
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			s.logger.Log("msg", "executing resume")
			if err := s.bouncer.Resume(ctx); err != nil {
				s.logger.Log("error", err, "msg", "failed to resume pgbouncer")
			}
		}()
	}

	return &PauseResponse{
		CreatedAt: mustTimestampProto(createdAt),
		ExpiresAt: mustTimestampProto(expiresAt),
	}, err
}

func (s *Server) Resume(ctx context.Context, _ *Empty) (*ResumeResponse, error) {
	if err := s.bouncer.Resume(ctx); err != nil {
		return nil, status.Errorf(codes.Unknown, "failed to resume pgbouncer: %s", err.Error())
	}

	return &ResumeResponse{CreatedAt: mustTimestampProto(time.Now())}, nil
}

func mustTimestampProto(t time.Time) *tspb.Timestamp {
	ts, err := ptypes.TimestampProto(t)

	if err != nil {
		panic("failed to convert what should have been an entirely safe timestamp")
	}

	return ts
}

func iso3339(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-0700")
}
