package ports

import (
	"context"

	"iot-platform/internal/model"
)

// CameraMappingStore keeps camera records and their relations to devices.
type CameraMappingStore interface {
	SaveVideoCameraMapping(context.Context, model.VideoCameraMapping) error
	GetVideoCameraMapping(context.Context, string, string) (model.VideoCameraMapping, error)
	ListVideoCameraMappings(context.Context, string) ([]model.VideoCameraMapping, error)
	ListVideoCameraMappingsByDeviceIDs(context.Context, string, []string) (map[string][]model.VideoCameraMapping, error)
	ListVideoCameraMappingsPage(context.Context, string, int, int) ([]model.VideoCameraMapping, int, error)
	ReplaceVideoCameraRelations(context.Context, string, string, []model.VideoCameraRelation) error
	ListVideoCameraRelations(context.Context, string, string) ([]model.VideoCameraRelation, error)
	ListVideoCameraRelationsByTarget(context.Context, string, string, string) ([]model.VideoCameraRelation, error)
}
