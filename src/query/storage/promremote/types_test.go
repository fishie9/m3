package promremote

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/m3db/m3/src/query/storage/m3"
	"github.com/m3db/m3/src/query/storage/m3/storagemetadata"
	"github.com/m3db/m3/src/x/ident"
)

var opts = Options{
	endpoints: []EndpointOptions{
		{
			name:          "raw",
			resolution:    0,
			retention:     0,
			downsampleAll: false,
		},
		{
			name:          "downsampled1",
			retention:     time.Second,
			resolution:    time.Millisecond,
			downsampleAll: true,
		},
		{
			name:          "downsampled2",
			retention:     time.Minute,
			resolution:    time.Hour,
			downsampleAll: false,
		},
	},
}

func TestNamespaces(t *testing.T) {
	ns := opts.Namespaces()
	downsampleTrue := true
	downsampleFalse := false

	assertNamespace(expectation{
		t:          t,
		ns:         ns[0],
		expectedID: "raw",
		expectedAttributes: storagemetadata.Attributes{
			Retention:   0,
			Resolution:  0,
			MetricsType: storagemetadata.UnaggregatedMetricsType,
		},
		expectedDownsample: nil,
	})

	assertNamespace(expectation{
		t:          t,
		ns:         ns[1],
		expectedID: "downsampled1",
		expectedAttributes: storagemetadata.Attributes{
			Retention:   time.Second,
			Resolution:  time.Millisecond,
			MetricsType: storagemetadata.AggregatedMetricsType,
		},
		expectedDownsample: &downsampleTrue,
	})

	assertNamespace(expectation{
		t:          t,
		ns:         ns[2],
		expectedID: "downsampled2",
		expectedAttributes: storagemetadata.Attributes{
			Retention:   time.Minute,
			Resolution:  time.Hour,
			MetricsType: storagemetadata.AggregatedMetricsType,
		},
		expectedDownsample: &downsampleFalse,
	})
}

func TestNewSessionPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("NewSession must panic")
		}
	}()

	opts.Namespaces()[0].Session()
}

type expectation struct {
	t                  *testing.T
	ns                 m3.ClusterNamespace
	expectedID         string
	expectedAttributes storagemetadata.Attributes
	expectedDownsample *bool
}

func assertNamespace(e expectation) {
	assert.Equal(e.t, ident.StringID(e.expectedID), e.ns.NamespaceID())
	assert.Equal(e.t, e.expectedAttributes, e.ns.Options().Attributes())
	if e.expectedDownsample != nil {
		ds, err := e.ns.Options().DownsampleOptions()
		require.NoError(e.t, err)
		assert.Equal(e.t, *e.expectedDownsample, ds.All)
	} else {
		_, err := e.ns.Options().DownsampleOptions()
		require.Error(e.t, err)
	}
}
