//go:build integration_v2
// +build integration_v2

// Copyright (c) 2021  Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package inprocess

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAggregator(t *testing.T) {
	_, nodeCloser := setupNode(t)
	defer nodeCloser()

	agg, err := NewAggregator(defaultAggregatorConfig, AggregatorOptions{})
	require.NoError(t, err)

	require.NoError(t, agg.Close())
}

const defaultAggregatorConfig = `
kvClient:
  etcd:
    env: default_env
    zone: embedded
    service: m3db
    cacheDir: "*"
    etcdClusters:
      - zone: embedded
        endpoints:
        - 127.0.0.1:2379
aggregator:
  client:
    type: m3msg
    m3msg:
      producer:
        writer:
          topicName: test
          topicServiceOverride:
            zone: embedded
            environment: default_env/test
          placement:
            isStaged: true
          placementServiceOverride:
            namespaces:
              placement: /placement
          messagePool:
            size: 16384
            watermark:
              low: 0.2
              high: 0.5
          ignoreCutoffCutover: true
`
