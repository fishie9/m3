// Copyright (c) 2016 Uber Technologies, Inc.
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

package m3tsz

import (
	"errors"
	"math"

	"github.com/m3db/m3/src/dbnode/encoding"
	"github.com/m3db/m3/src/dbnode/namespace"
	"github.com/m3db/m3/src/dbnode/ts"
	"github.com/m3db/m3/src/dbnode/x/xio"
	xtime "github.com/m3db/m3/src/x/time"
)

var errClosed = errors.New("iterator is closed")

// DefaultReaderIteratorAllocFn returns a function for allocating NewReaderIterator.
func DefaultReaderIteratorAllocFn(
	opts encoding.Options,
) func(r xio.Reader64, _ namespace.SchemaDescr) encoding.ReaderIterator {
	return func(r xio.Reader64, _ namespace.SchemaDescr) encoding.ReaderIterator {
		return NewReaderIterator(r, DefaultIntOptimizationEnabled, opts)
	}
}

// readerIterator provides an interface for clients to incrementally
// read datapoints off of an encoded stream.
type readerIterator struct {
	is   *encoding.IStream
	opts encoding.Options

	err        error   // current error
	intVal     float64 // current int value
	tsIterator TimestampIterator
	floatIter  FloatEncoderAndIterator

	mult uint8 // current int multiplier
	sig  uint8 // current number of significant bits for int diff

	curr         ts.Datapoint
	intOptimized bool // whether encoding scheme is optimized for ints
	isFloat      bool // whether encoding is in int or float

	closed bool
}

// NewReaderIterator returns a new iterator for a given reader
func NewReaderIterator(
	reader xio.Reader64,
	intOptimized bool,
	opts encoding.Options,
) encoding.ReaderIterator {
	return &readerIterator{
		is:           encoding.NewIStream(reader),
		opts:         opts,
		tsIterator:   NewTimestampIterator(opts, false),
		intOptimized: intOptimized,
	}
}

// Next moves to the next item
func (it *readerIterator) Next() bool {
	if !it.hasNext() {
		return false
	}

	if it.tsIterator.PrevTime != 0 && !it.tsIterator.TimeUnitChanged {
		if it.intOptimized {
			// This is the fast path to read the shortest encoding possible (001 binary) which
			// represents an unchanged datapoint value and unchanged delta (deltaOfDelta = 0).
			//  Breakdown of 001 opcode sequence:
			// 0: TimeEncodingScheme.ZeroBucket().Opcode()
			// 0: opcodeUpdate
			// 1: opcodeRepeat
			if it.peekBits(3) == 0b001 {
				if it.err != nil {
					return false
				}

				it.tsIterator.PrevTime += xtime.UnixNano(it.tsIterator.PrevTimeDelta)
				it.curr.TimestampNanos = it.tsIterator.PrevTime
				it.tsIterator.PrevAnt = nil

				it.readBits(3) // consume the bits that were checked with peekBits

				return it.hasNext()
			}
		} else { // non-int optimized
			// This is the fast path to read the shortest encoding possible (00 binary) which
			// represents an unchanged datapoint value and unchanged delta (deltaOfDelta = 0).
			//  Breakdown of 00 opcode sequence:
			// 0: TimeEncodingScheme.ZeroBucket().Opcode()
			// 0: opcodeZeroValueXOR
			if it.peekBits(2) == 0b00 {
				if it.err != nil {
					return false
				}

				it.tsIterator.PrevTime += xtime.UnixNano(it.tsIterator.PrevTimeDelta)
				it.curr.TimestampNanos = it.tsIterator.PrevTime
				it.floatIter.PrevXOR = 0
				it.tsIterator.PrevAnt = nil

				it.readBits(2) // consume the bits that were checked with peekBits

				return it.hasNext()
			}
		}
	}

	first, done, err := it.tsIterator.ReadTimestamp(it.is)
	if err != nil || done {
		it.err = err
		return false
	}

	if !first {
		it.readNextValue()
	} else {
		it.readFirstValue()
	}

	it.curr.TimestampNanos = it.tsIterator.PrevTime
	if !it.intOptimized || it.isFloat {
		it.curr.Value = math.Float64frombits(it.floatIter.PrevFloatBits)
	} else {
		it.curr.Value = convertFromIntFloat(it.intVal, it.mult)
	}

	return it.hasNext()
}

func (it *readerIterator) readFirstValue() {
	if !it.intOptimized {
		if err := it.floatIter.readFullFloat(it.is); err != nil {
			it.err = err
		}
		return
	}

	if it.readBits(1) == opcodeFloatMode {
		if err := it.floatIter.readFullFloat(it.is); err != nil {
			it.err = err
		}
		it.isFloat = true
		return
	}

	it.readIntSigMult()
	it.readIntValDiff()
}

func (it *readerIterator) readNextValue() {
	if !it.intOptimized {
		if err := it.floatIter.readNextFloat(it.is); err != nil {
			it.err = err
		}
		return
	}

	if it.readBits(1) == opcodeUpdate {
		if it.readBits(1) == opcodeRepeat {
			return
		}

		if it.readBits(1) == opcodeFloatMode {
			// Change to floatVal
			if err := it.floatIter.readFullFloat(it.is); err != nil {
				it.err = err
			}
			it.isFloat = true
			return
		}

		it.readIntSigMult()
		it.readIntValDiff()
		it.isFloat = false
		return
	}

	if it.isFloat {
		if err := it.floatIter.readNextFloat(it.is); err != nil {
			it.err = err
		}
		return
	}

	// inlined readIntValDiff()
	if it.sig == 64 {
		it.readIntValDiffSlow()
		return
	}
	bits := it.readBits(it.sig + 1)
	sign := -1.0
	if (bits >> it.sig) == opcodeNegative {
		sign = 1.0
		// clear the opcode bit
		bits ^= uint64(1 << it.sig)
	}
	it.intVal += sign * float64(bits)
}

func (it *readerIterator) readIntSigMult() {
	if it.readBits(1) == opcodeUpdateSig {
		if it.readBits(1) == OpcodeZeroSig {
			it.sig = 0
		} else {
			it.sig = uint8(it.readBits(NumSigBits)) + 1
		}
	}

	if it.readBits(1) == opcodeUpdateMult {
		it.mult = uint8(it.readBits(numMultBits))
		if it.mult > maxMult {
			it.err = errInvalidMultiplier
		}
	}
}

func (it *readerIterator) readIntValDiff() {
	// check if we can read both sign bit and digits in one read
	if it.sig == 64 {
		it.readIntValDiffSlow()
		return
	}
	// read both sign bit and digits in one read
	bits := it.readBits(it.sig + 1)
	sign := -1.0
	if (bits >> it.sig) == opcodeNegative {
		sign = 1.0
		// clear the opcode bit
		bits ^= uint64(1 << it.sig)
	}
	it.intVal += sign * float64(bits)
}

func (it *readerIterator) readIntValDiffSlow() {
	sign := -1.0
	if it.readBits(1) == opcodeNegative {
		sign = 1.0
	}

	it.intVal += sign * float64(it.readBits(it.sig))
}

func (it *readerIterator) readBits(numBits uint8) (res uint64) {
	res, it.err = it.is.ReadBits(numBits)
	return
}

func (it *readerIterator) peekBits(numBits uint8) (res uint64) {
	res, it.err = it.is.PeekBits(numBits)
	return
}

// Current returns the value as well as the annotation associated with the current datapoint.
// Users should not hold on to the returned Annotation object as it may get invalidated when
// the iterator calls Next().
func (it *readerIterator) Current() (ts.Datapoint, xtime.Unit, ts.Annotation) {
	return it.curr, it.tsIterator.TimeUnit, it.tsIterator.PrevAnt
}

// Err returns the error encountered
func (it *readerIterator) Err() error {
	return it.err
}

func (it *readerIterator) hasError() bool {
	return it.err != nil
}

func (it *readerIterator) isDone() bool {
	return it.tsIterator.Done
}

func (it *readerIterator) isClosed() bool {
	return it.closed
}

func (it *readerIterator) hasNext() bool {
	return !it.hasError() && !it.isDone()
}

// Reset resets the ReadIterator for reuse.
func (it *readerIterator) Reset(reader xio.Reader64, schema namespace.SchemaDescr) {
	it.is.Reset(reader)
	it.tsIterator = NewTimestampIterator(it.opts, it.tsIterator.SkipMarkers)
	it.err = nil
	it.isFloat = false
	it.intVal = 0.0
	it.mult = 0
	it.sig = 0
	it.closed = false
}

// Close closes the ReaderIterator.
func (it *readerIterator) Close() {
	if it.closed {
		return
	}

	it.closed = true
	it.err = errClosed
	pool := it.opts.ReaderIteratorPool()
	if pool != nil {
		pool.Put(it)
	}
}
