package manager

import (
	"testing"
	"time"

	"github.com/nlewo/comin/pkg/protobuf"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSelectNiks3Pin(t *testing.T) {
	now := time.Now()
	older := &protobuf.PinInfo{Name: "a", StorePath: "/nix/store/a", UpdatedAt: timestamppb.New(now.Add(-time.Minute))}
	newer := &protobuf.PinInfo{Name: "b", StorePath: "/nix/store/b", UpdatedAt: timestamppb.New(now)}

	assert.Nil(t, selectNiks3Pin(nil))
	assert.Nil(t, selectNiks3Pin(&protobuf.Niks3Status{Remotes: []*protobuf.Niks3Remote{{Name: "a"}}}))
	assert.Equal(t, newer, selectNiks3Pin(&protobuf.Niks3Status{Remotes: []*protobuf.Niks3Remote{
		{Name: "a", Pininfo: older},
		{Name: "unfetched"},
		{Name: "b", Pininfo: newer},
	}}))
}

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
