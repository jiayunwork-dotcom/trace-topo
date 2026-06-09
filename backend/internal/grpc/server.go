package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	v1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

type SpanProcessor interface {
	ProcessSpans(ctx context.Context, spans []*model.Span) error
	ShouldBackpressure() bool
}

type TraceReceiverServer struct {
	collectortrace.UnimplementedTraceServiceServer
	processor SpanProcessor
	server    *grpc.Server
	mu        sync.RWMutex
}

func NewTraceReceiverServer(processor SpanProcessor) *TraceReceiverServer {
	return &TraceReceiverServer{
		processor: processor,
	}
}

func (s *TraceReceiverServer) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.AppConfig.GRPCPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(1024 * 1024 * 16),
		grpc.MaxConcurrentStreams(1024),
	}

	s.server = grpc.NewServer(opts...)
	collectortrace.RegisterTraceServiceServer(s.server, s)

	logrus.Infof("OTLP gRPC server starting on port %d", config.AppConfig.GRPCPort)

	go func() {
		if err := s.server.Serve(lis); err != nil {
			logrus.Errorf("gRPC server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.server.GracefulStop()
		logrus.Info("gRPC server stopped gracefully")
	}()

	return nil
}

func (s *TraceReceiverServer) Export(
	ctx context.Context,
	req *collectortrace.ExportTraceServiceRequest,
) (*collectortrace.ExportTraceServiceResponse, error) {
	if req == nil || req.ResourceSpans == nil {
		return &collectortrace.ExportTraceServiceResponse{}, nil
	}

	var allSpans []*model.Span

	for _, rs := range req.ResourceSpans {
		serviceName := getServiceName(rs)

		for _, scopeSpans := range rs.ScopeSpans {
			for _, span := range scopeSpans.Spans {
				if len(allSpans) >= config.AppConfig.MaxBatchSize {
					logrus.Warnf("Batch size exceeds limit %d, applying backpressure", config.AppConfig.MaxBatchSize)
					return nil, status.Errorf(
						codes.ResourceExhausted,
						"batch size exceeds limit of %d, please reduce send rate",
						config.AppConfig.MaxBatchSize,
					)
				}

				modelSpan := convertOTLPSpan(span, serviceName)
				if modelSpan != nil {
					allSpans = append(allSpans, modelSpan)
				}
			}
		}
	}

	if len(allSpans) > 0 {
		if s.processor.ShouldBackpressure() {
			return nil, status.Errorf(
				codes.ResourceExhausted,
				"system is overloaded, please retry later",
			)
		}

		if err := s.processor.ProcessSpans(ctx, allSpans); err != nil {
			logrus.Errorf("Failed to process spans: %v", err)
			return nil, status.Errorf(codes.Internal, "failed to process spans: %v", err)
		}
	}

	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get("x-request-id")) > 0 {
		header := metadata.Pairs("x-request-id", md.Get("x-request-id")[0])
		grpc.SendHeader(ctx, header)
	}

	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func convertOTLPSpan(span *v1.Span, serviceName string) *model.Span {
	if span == nil {
		return nil
	}

	traceID := bytesToHex(span.TraceId)
	spanID := bytesToHex(span.SpanId)
	parentSpanID := bytesToHex(span.ParentSpanId)

	if traceID == "" || spanID == "" {
		logrus.Warn("Invalid span: missing trace_id or span_id")
		return nil
	}

	startTime := time.Unix(0, int64(span.StartTimeUnixNano))
	endTime := time.Unix(0, int64(span.EndTimeUnixNano))
	duration := endTime.Sub(startTime).Milliseconds()

	if duration < 0 {
		duration = 0
	}

	attrs := make(map[string]interface{})
	for _, attr := range span.Attributes {
		attrs[attr.Key] = getAttributeValue(attr)
	}

	statusCode := int32(0)
	if span.Status != nil {
		statusCode = int32(span.Status.Code)
	}

	return &model.Span{
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parentSpanID,
		ServiceName:   serviceName,
		OperationName: span.Name,
		StartTime:     startTime,
		EndTime:       endTime,
		DurationMs:    duration,
		StatusCode:    statusCode,
		Attributes:    attrs,
		CreatedAt:     time.Now(),
	}
}

func getServiceName(rs *v1.ResourceSpans) string {
	if rs == nil || rs.Resource == nil {
		return "unknown"
	}

	for _, attr := range rs.Resource.Attributes {
		if attr.Key == "service.name" {
			if val := getAttributeValue(attr); val != nil {
				if s, ok := val.(string); ok {
					return s
				}
			}
		}
	}
	return "unknown"
}

func getAttributeValue(attr interface{ GetValue() *v1.AnyValue }) interface{} {
	val := attr.GetValue()
	if val == nil {
		return nil
	}

	switch v := val.Value.(type) {
	case *v1.AnyValue_StringValue:
		return v.StringValue
	case *v1.AnyValue_BoolValue:
		return v.BoolValue
	case *v1.AnyValue_IntValue:
		return v.IntValue
	case *v1.AnyValue_DoubleValue:
		return v.DoubleValue
	default:
		return nil
	}
}

func bytesToHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return fmt.Sprintf("%x", b)
}

func (s *TraceReceiverServer) GetMetrics(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
