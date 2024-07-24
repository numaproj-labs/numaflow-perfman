package collect

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/spf13/afero"
)

var mockSampleStream1 = model.SampleStream{}

var mockMatrix model.Matrix = []*model.SampleStream{&mockSampleStream1}

func TestWriteToDumpFile(t *testing.T) {
	appFS := afero.NewMemMapFs()
}
