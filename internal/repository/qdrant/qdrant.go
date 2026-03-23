package qdrant

import (
	"context"
	"fmt"
	pb "github.com/qdrant/go-client/qdrant"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"rag_robot/internal/pkg/circuitbreaker"
	"rag_robot/internal/pkg/tracing"
)

const (
	collectionName  = "rag_chunks"
	vectorDimension = 1536 // OpenAI text-embedding-ada-002
)

// Client Qdrant gRPC 客户端封装。
type Client struct {
	conn        *grpc.ClientConn
	collections pb.CollectionsClient
	points      pb.PointsClient
	breaker     *circuitbreaker.CircuitBreaker
}

// WithBreaker 挂载熔断器，返回自身方便链式调用。
func (c *Client) WithBreaker(cb *circuitbreaker.CircuitBreaker) *Client {
	c.breaker = cb
	return c
}

// NewClient 初始化并连接 Qdrant，确保 Collection 存在。
func NewClient(host string, port int) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("连接 Qdrant 失败: %w", err)
	}

	c := &Client{
		conn:        conn,
		collections: pb.NewCollectionsClient(conn),
		points:      pb.NewPointsClient(conn),
	}

	if err = c.ensureCollection(context.Background()); err != nil {
		conn.Close()
		return nil, err
	}

	return c, nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	return c.conn.Close()
}

// ensureCollection 如不存在则创建默认集合。
func (c *Client) ensureCollection(ctx context.Context) error {
	resp, err := c.collections.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("查询 Qdrant Collections 失败: %w", err)
	}
	for _, col := range resp.Collections {
		if col.Name == collectionName {
			return nil
		}
	}

	distance := pb.Distance_Cosine
	_, err = c.collections.Create(ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{
					Size:     vectorDimension,
					Distance: distance,
					HnswConfig: &pb.HnswConfigDiff{
						M:           ptr(uint64(16)),
						EfConstruct: ptr(uint64(200)),
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("创建 Qdrant Collection 失败: %w", err)
	}
	return nil
}

// ChunkPoint 表示要写入 Qdrant 的一个分块向量点。
type ChunkPoint struct {
	ID              uint64
	ChunkID         int64
	Vector          []float32
	DocumentID      int64
	KnowledgeBaseID int64
	ChunkIndex      int
	Content         string
}

// UpsertPoints 批量写入向量点。
func (c *Client) UpsertPoints(ctx context.Context, points []*ChunkPoint) error {
	if len(points) == 0 {
		return nil
	}

	ctx, span := tracing.Tracer.Start(ctx, "qdrant.UpsertPoints")
	defer span.End()
	span.SetAttributes(attribute.Int("qdrant.points_count", len(points)))

	call := func() error {
		pbPoints := make([]*pb.PointStruct, 0, len(points))
		for _, p := range points {
			pbPoints = append(pbPoints, &pb.PointStruct{
				Id: &pb.PointId{PointIdOptions: &pb.PointId_Num{Num: p.ID}},
				Vectors: &pb.Vectors{
					VectorsOptions: &pb.Vectors_Vector{
						Vector: &pb.Vector{Data: p.Vector},
					},
				},
				Payload: map[string]*pb.Value{
					"chunk_id":          {Kind: &pb.Value_IntegerValue{IntegerValue: p.ChunkID}},
					"document_id":       {Kind: &pb.Value_IntegerValue{IntegerValue: p.DocumentID}},
					"knowledge_base_id": {Kind: &pb.Value_IntegerValue{IntegerValue: p.KnowledgeBaseID}},
					"chunk_index":       {Kind: &pb.Value_IntegerValue{IntegerValue: int64(p.ChunkIndex)}},
					"content":           {Kind: &pb.Value_StringValue{StringValue: p.Content}},
				},
			})
		}

		waitUpsert := true
		_, err := c.points.Upsert(ctx, &pb.UpsertPoints{
			CollectionName: collectionName,
			Wait:           &waitUpsert,
			Points:         pbPoints,
		})
		if err != nil {
			return fmt.Errorf("写入 Qdrant 失败: %w", err)
		}
		return nil
	}

	if c.breaker != nil {
		if err := c.breaker.Call(call); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		return nil
	}
	if err := call(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// SearchResult 表示检索结果。
type SearchResult struct {
	PointID         string
	ChunkID         int64
	Score           float32
	DocumentID      int64
	KnowledgeBaseID int64
	ChunkIndex      int
	Content         string
}

// Search 执行 Top-K 相似度检索，并按 knowledge_base_id 过滤。
func (c *Client) Search(ctx context.Context, vector []float32, kbID int64, topK uint64) ([]*SearchResult, error) {
	ctx, span := tracing.Tracer.Start(ctx, "qdrant.Search")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("qdrant.kb_id", kbID),
		attribute.Int64("qdrant.top_k", int64(topK)),
	)

	var results []*SearchResult

	call := func() error {
		resp, err := c.points.Search(ctx, &pb.SearchPoints{
			CollectionName: collectionName,
			Vector:         vector,
			Limit:          topK,
			WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
			Filter: &pb.Filter{
				Must: []*pb.Condition{
					{
						ConditionOneOf: &pb.Condition_Field{
							Field: &pb.FieldCondition{
								Key: "knowledge_base_id",
								Match: &pb.Match{
									MatchValue: &pb.Match_Integer{Integer: kbID},
								},
							},
						},
					},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("Qdrant 检索失败: %w", err)
		}

		results = make([]*SearchResult, 0, len(resp.Result))
		for _, hit := range resp.Result {
			r := &SearchResult{
				ChunkID:         hit.Payload["chunk_id"].GetIntegerValue(),
				Score:           hit.Score,
				Content:         hit.Payload["content"].GetStringValue(),
				DocumentID:      hit.Payload["document_id"].GetIntegerValue(),
				KnowledgeBaseID: hit.Payload["knowledge_base_id"].GetIntegerValue(),
				ChunkIndex:      int(hit.Payload["chunk_index"].GetIntegerValue()),
			}
			if id, ok := hit.Id.PointIdOptions.(*pb.PointId_Num); ok {
				r.PointID = fmt.Sprintf("%d", id.Num)
			}
			results = append(results, r)
		}
		return nil
	}

	if c.breaker != nil {
		if err := c.breaker.Call(call); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		span.SetAttributes(attribute.Int("qdrant.hits", len(results)))
		return results, nil
	}
	if err := call(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("qdrant.hits", len(results)))
	return results, nil
}

// DeleteByDocumentID 删除某个文档的所有向量点。
func (c *Client) DeleteByDocumentID(ctx context.Context, documentID int64) error {
	waitDelete := true
	_, err := c.points.Delete(ctx, &pb.DeletePoints{
		CollectionName: collectionName,
		Wait:           &waitDelete,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Filter{
				Filter: &pb.Filter{
					Must: []*pb.Condition{
						{
							ConditionOneOf: &pb.Condition_Field{
								Field: &pb.FieldCondition{
									Key: "document_id",
									Match: &pb.Match{
										MatchValue: &pb.Match_Integer{Integer: documentID},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("删除 Qdrant 向量点失败: %w", err)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// Ping 检查 Qdrant 是否可达，用于健康检查。
// 通过 List collections 接口探测连通性，有响应即视为正常。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.collections.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("qdrant ping 失败: %w", err)
	}
	return nil
}
