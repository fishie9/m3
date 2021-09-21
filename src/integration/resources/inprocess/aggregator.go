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
	"errors"
	"time"

	m3agg "github.com/m3db/m3/src/aggregator/aggregator"
	"github.com/m3db/m3/src/aggregator/server"
	"github.com/m3db/m3/src/cmd/services/m3aggregator/config"
	"github.com/m3db/m3/src/integration/resources"
	xos "github.com/m3db/m3/src/x/os"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type aggregator struct {
	cfg    config.Configuration
	logger *zap.Logger
	// tmpDirs []string

	interruptCh chan<- error
	shutdownCh  <-chan struct{}
}

// AggregatorOptions are options of starting an in-process aggregator.
type AggregatorOptions struct {
	// Logger is the logger to use for the in-process aggregator.
	Logger *zap.Logger
}

func NewAggregator(yamlCfg string, opts AggregatorOptions) (resources.Aggregator, error) {
	var cfg config.Configuration
	if err := yaml.Unmarshal([]byte(yamlCfg), &cfg); err != nil {
		return nil, err
	}

	// todo: update ports, dirs,

	if opts.Logger == nil {
		var err error
		opts.Logger, err = zap.NewDevelopment()
		if err != nil {
			return nil, err
		}
	}

	agg := &aggregator{
		cfg:    cfg,
		logger: opts.Logger,
		// tmpDirs: tmpDirs,
	}
	agg.start()

	return agg, nil
}

func (a *aggregator) IsHealthy(instance string) error {
	return nil
}

func (a *aggregator) Status(instance string) (m3agg.RuntimeStatus, error) {
	return m3agg.RuntimeStatus{}, nil
}

func (a *aggregator) Resign(instance string) error {
	return nil
}

func (a *aggregator) Close() error {
	// defer func() {
	// 	for _, dir := range a.tmpDirs {
	// 		if err := os.RemoveAll(dir); err != nil {
	// 			a.logger.Error("error removing temp directory", zap.String("dir", dir), zap.Error(err))
	// 		}
	// 	}
	// }()

	select {
	case a.interruptCh <- xos.NewInterruptError("in-process aggregator being shut down"):
	case <-time.After(interruptTimeout):
		return errors.New("timeout sending interrupt. closing without graceful shutdown")
	}

	select {
	case <-a.shutdownCh:
	case <-time.After(shutdownTimeout):
		return errors.New("timeout waiting for shutdown notification. server closing may" +
			" not be completely graceful")
	}

	return nil
}

func (a *aggregator) start() {
	interruptCh := make(chan error, 1)
	shutdownCh := make(chan struct{}, 1)

	go func() {
		server.Run(server.RunOptions{
			Config:      a.cfg,
			InterruptCh: interruptCh,
			ShutdownCh:  shutdownCh,
		})
	}()

	a.interruptCh = interruptCh
	a.shutdownCh = shutdownCh
}
