package clarifai

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	pb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api"
	statuspb "github.com/Clarifai/clarifai-go-grpc/proto/clarifai/api/status"
	"google.golang.org/protobuf/proto"
)

// GetInput fetches a specific input from the Clarifai API.
func (c *Client) GetInput(ctx context.Context, userAppID *pb.UserAppIDSet, inputID string, logger *slog.Logger) (*pb.Input, error) {
	logger.Debug("Calling GetInput", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "input_id", inputID)
	grpcRequest := &pb.GetInputRequest{UserAppId: userAppID, InputId: inputID}
	resp, err := c.API.GetInput(ctx, grpcRequest)
	if err != nil {
		return nil, err
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		return nil, NewAPIStatusError(resp.GetStatus())
	}
	return resp.Input, nil
}

// GetModel fetches a specific model from the Clarifai API.
func (c *Client) GetModel(ctx context.Context, userAppID *pb.UserAppIDSet, modelID string, logger *slog.Logger) (*pb.Model, error) {
	logger.Debug("Calling GetModel", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "model_id", modelID)
	grpcRequest := &pb.GetModelRequest{UserAppId: userAppID, ModelId: modelID}
	resp, err := c.API.GetModel(ctx, grpcRequest)
	if err != nil {
		return nil, err
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		return nil, NewAPIStatusError(resp.GetStatus())
	}
	return resp.Model, nil
}

// ListInputs lists or searches inputs from the Clarifai API.
func (c *Client) ListInputs(ctx context.Context, userAppID *pb.UserAppIDSet, pagination *pb.Pagination, query string, logger *slog.Logger) ([]proto.Message, string, error) {
	var results []proto.Message
	var nextCursor string
	var apiErr error

	if query != "" {
		logger.Debug("Calling PostInputsSearches", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "query", query, "page", pagination.Page, "per_page", pagination.PerPage)
		searchQueryProto := &pb.Query{Ranks: []*pb.Rank{{Annotation: &pb.Annotation{Data: &pb.Data{Text: &pb.Text{Raw: query}}}}}}
		grpcRequest := &pb.PostInputsSearchesRequest{UserAppId: userAppID, Searches: []*pb.Search{{Query: searchQueryProto}}, Pagination: pagination}
		resp, err := c.API.PostInputsSearches(ctx, grpcRequest)
		apiErr = err
		if err == nil { // No API call error, check status
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				apiErr = NewAPIStatusError(resp.GetStatus())
			} else {
				results = make([]proto.Message, 0, len(resp.Hits))
				for _, hit := range resp.Hits {
					if hit.Input != nil {
						results = append(results, hit.Input)
					}
				}
				if uint32(len(resp.Hits)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		}
	} else {
		logger.Debug("Calling ListInputs", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "page", pagination.Page, "per_page", pagination.PerPage)
		grpcRequest := &pb.ListInputsRequest{UserAppId: userAppID, Page: pagination.Page, PerPage: pagination.PerPage}
		resp, err := c.API.ListInputs(ctx, grpcRequest)
		apiErr = err
		if err == nil { // No API call error, check status
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				apiErr = NewAPIStatusError(resp.GetStatus())
			} else {
				results = make([]proto.Message, 0, len(resp.Inputs))
				for _, input := range resp.Inputs {
					results = append(results, input)
				}
				if uint32(len(resp.Inputs)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		}
	}
	return results, nextCursor, apiErr
}

// PostInputs uploads new inputs to the Clarifai API.
func (c *Client) PostInputs(ctx context.Context, userAppID *pb.UserAppIDSet, inputs []*pb.Input, logger *slog.Logger) (*pb.MultiInputResponse, error) { // Changed response type
	logger.Debug("Calling PostInputs", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "input_count", len(inputs))
	grpcRequest := &pb.PostInputsRequest{UserAppId: userAppID, Inputs: inputs}
	resp, err := c.API.PostInputs(ctx, grpcRequest) // Assuming the method call itself is correct now
	if err != nil {
		return nil, err
	}
	if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
		return nil, NewAPIStatusError(resp.GetStatus())
	}
	return resp, nil
}

// ListModels lists models from the Clarifai API. Querying is not currently supported here.
func (c *Client) ListModels(ctx context.Context, userAppID *pb.UserAppIDSet, pagination *pb.Pagination, query string, logger *slog.Logger) ([]proto.Message, string, error) {
	var results []proto.Message
	var nextCursor string
	var apiErr error

	if query != "" {
		logger.Warn("Query parameter is not yet supported for listing models", "query", query)
		apiErr = fmt.Errorf("query parameter not supported for listing models")
	} else {
		logger.Debug("Calling ListModels", "user_id", userAppID.UserId, "app_id", userAppID.AppId, "page", pagination.Page, "per_page", pagination.PerPage)
		grpcRequest := &pb.ListModelsRequest{UserAppId: userAppID, Page: pagination.Page, PerPage: pagination.PerPage}
		resp, err := c.API.ListModels(ctx, grpcRequest)
		apiErr = err
		if err == nil { // No API call error, check status
			if resp.GetStatus().GetCode() != statuspb.StatusCode_SUCCESS {
				apiErr = NewAPIStatusError(resp.GetStatus())
			} else {
				results = make([]proto.Message, 0, len(resp.Models))
				for _, model := range resp.Models {
					results = append(results, model)
				}
				if uint32(len(resp.Models)) == pagination.PerPage {
					nextCursor = strconv.Itoa(int(pagination.Page + 1))
				}
			}
		}
	}
	return results, nextCursor, apiErr
}
