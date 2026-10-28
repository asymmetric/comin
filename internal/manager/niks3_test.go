package manager

import (
	"testing"

	"github.com/nlewo/comin/pkg/protobuf"
	"github.com/stretchr/testify/assert"
)

func TestGetOperation(t *testing.T) {
	m := &Manager{configurationOperations: ConfigurationOperations{
		"origin": {"main": "switch", "testing": "test"},
		"prod":   {"": "boot"},
	}}
	niks3 := &protobuf.Generation{Source: &protobuf.Source{Source: &protobuf.Source_Niks3{Niks3: &protobuf.PinInfo{Name: "prod"}}}}
	git := &protobuf.Generation{Source: &protobuf.Source{Source: &protobuf.Source_Git{Git: &protobuf.Git{SelectedRemoteName: "origin", SelectedBranchName: "main"}}}}

	assert.Equal(t, "boot", m.getOperation(niks3))
	assert.Equal(t, "switch", m.getOperation(git))
	// Unknown sources fall back to the non-persistent operation.
	assert.Equal(t, "test", m.getOperation(&protobuf.Generation{}))
}
