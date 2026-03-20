package qdrant

import (
	"context"
	"fmt"

	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	collectionName  = "rag_chunks"
	vectorDimension = 1536 // OpenAI text-embedding-ada-002 维度
)

// Client Qdrant gRPC 客户端封装
type Client struct {
	conn        *grpc.ClientConn
	collections pb.CollectionsClient
	points      pb.PointsClient
}

// NewClient 初始化并连接 Qdrant，确保 Collection 存在
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

// Close 关闭连接
func (c *Client) Close() error {
	return c.conn.Close()
}

// ensureCollection 如果 Collection 不存在则创建
func (c *Client) ensureCollection(ctx context.Context) error {
	// 先查询是否存在
	resp, err := c.collections.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("查询 Qdrant Collections 失败: %w", err)
	}
	for _, col := range resp.Collections {
		if col.Name == collectionName {
			return nil // 已存在，无需创建
		}
	}

	// 不存在，创建
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

// ChunkPoint 写入 Qdrant 的一个数据点
type ChunkPoint struct {
	ID              uint64 // 数字ID，由 docID*100000+chunkIndex 生成
	Vector          []float32
	DocumentID      int64
	KnowledgeBaseID int64
	ChunkIndex      int
	Content         string
}

// UpsertPoints 批量写入向量点（存在则更新，不存在则插入）
func (c *Client) UpsertPoints(ctx context.Context, points []*ChunkPoint) error {
	if len(points) == 0 {
		return nil
	}

	pbPoints := make([]*pb.PointStruct, 0, len(points))
	for _, p := range points {
		pbPoints = append(pbPoints, &pb.PointStruct{
			Id: &pb.PointId{
				PointIdOptions: &pb.PointId_Num{Num: p.ID},
			},
			Vectors: &pb.Vectors{
				VectorsOptions: &pb.Vectors_Vector{
					Vector: &pb.Vector{Data: p.Vector},
				},
			},
			Payload: map[string]*pb.Value{
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

// SearchResult 检索结果
type SearchResult struct {
	PointID         string
	Score           float32
	DocumentID      int64
	KnowledgeBaseID int64
	ChunkIndex      int
	Content         string
}

// Search 余弦相似度 Top-K 检索，按 knowledge_base_id 过滤
func (c *Client) Search(ctx context.Context, vector []float32, kbID int64, topK uint64) ([]*SearchResult, error) {
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
		return nil, fmt.Errorf("Qdrant 检索失败: %w", err)
	}

	results := make([]*SearchResult, 0, len(resp.Result))
	for _, hit := range resp.Result {
		r := &SearchResult{
			Score:   hit.Score,
			Content: hit.Payload["content"].GetStringValue(),
		}
		if id, ok := hit.Id.PointIdOptions.(*pb.PointId_Num); ok {
			r.PointID = fmt.Sprintf("%d", id.Num)
		}
		r.DocumentID = hit.Payload["document_id"].GetIntegerValue()
		r.KnowledgeBaseID = hit.Payload["knowledge_base_id"].GetIntegerValue()
		r.ChunkIndex = int(hit.Payload["chunk_index"].GetIntegerValue())
		results = append(results, r)
	}
	return results, nil
}

// DeleteByDocumentID 删除某个文档的所有向量点
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
