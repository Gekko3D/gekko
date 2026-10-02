package gekko

import (
	"reflect"
	"testing"
)

func TestS3eComponentRevisionUnmarkedWritesReachExistingVoxelBridge(t *testing.T) {
	f := newS3cVoxelFixture(t)
	entity := f.add(1)
	f.app.FlushCommands()
	f.sync()
	obj := s3cResident(t, f, entity, 1, f.model)
	transformType := reflect.TypeOf(TransformComponent{})
	modelType := reflect.TypeOf(VoxelModelComponent{})
	transformRevision := f.cmd.ComponentRevision(transformType)
	modelRevision := f.cmd.ComponentRevision(modelType)
	structural := f.cmd.StructuralRevision()
	s3cComponent[TransformComponent](t, f.cmd, entity).Position[0] = 7
	model := s3cComponent[VoxelModelComponent](t, f.cmd, entity)
	geometry := f.server.CreateCubeModel(2, 3, 5, 1)
	model.VoxelModel, model.DisableShadows = geometry, true
	f.sync()
	if s3cResident(t, f, entity, 7, geometry) != obj || obj.CastsShadows {
		t.Fatal("unmarked transform/model fields must reach the existing live bridge")
	}
	if f.cmd.ComponentRevision(transformType) != transformRevision || f.cmd.ComponentRevision(modelType) != modelRevision || f.cmd.StructuralRevision() != structural {
		t.Fatal("direct writes and bridge reads must leave publication and structural revisions unchanged")
	}
}
