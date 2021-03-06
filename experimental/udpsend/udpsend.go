// Sample program measures the cost to create a small gogo protobuf,
// serialize, and send a UDP packet.  Produces an [W x R] matrix
// where:
//
// W: number of work units
// R: repetitions
package main

import (
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/lightstep/lightstep-benchmarks/benchlib"
)

const (
	roughTrials = 10
	numTrials   = 50000

	numKeys = 10
	keySize = 10
	valSize = 10

	maxWorkParam   = 10000
	maxRepeatParam = 10
)

var (
	roughEstimate benchlib.Time // estimate by testing.Benchmark()
	workFactor    int           // someWork(workFactor) takes ~roughEstimate

	workParams   = append(iRange(100, 1900, 100), iRange(2000, 10000, 1000)...)
	repeatParams = iRange(10, 10, 1)
)

type tParam struct {
	parameter  int
	iterations int
	featureOn  int // 0 or 1
}

type tResults [2]tExperiment
type tExperiment [maxWorkParam + 1][maxRepeatParam + 1][]benchlib.Timing

func iRange(low, high, step int) []int {
	var r []int
	for i := low; i <= high; i += step {
		r = append(r, i)
	}
	return r
}

func someWork(c int) int32 {
	s := int32(1)
	for ; c != 0; c-- {
		s *= 982451653
	}
	return s
}

func udpSend(id int32, conn *net.UDPConn) {
	r := &Report{}
	r.Id = id
	r.Field = make([]*KeyValue, numKeys)
	for i, _ := range r.Field {
		r.Field[i] = &KeyValue{strings.Repeat("k", keySize), strings.Repeat("v", valSize)}
	}
	d, err := proto.Marshal(r)
	if err != nil {
		panic(err.Error())
	}
	conn.Write(d)
}

func connectUDP() *net.UDPConn {
	udpAddr, err := net.ResolveUDPAddr("udp", "localhost:1026")
	if err != nil {
		panic(err.Error())
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		panic(err.Error())
	}
	return conn
}

func getParams() []tParam {
	var params []tParam
	for _, w := range workParams {
		for _, r := range repeatParams {
			for t := 0; t < numTrials; t++ {
				params = append(params, tParam{w, r, 1})
				params = append(params, tParam{w, r, 0})
			}
		}
	}
	for i := 1; i < len(params); i++ {
		ri := rand.Intn(i)
		params[ri], params[i] = params[i], params[ri]
	}
	return params
}

func emptyResults() *tResults {
	results := &tResults{}
	for on := 0; on < 2; on++ {
		exp := tExperiment{}
		for _, w := range workParams {
			for _, r := range repeatParams {
				exp[w][r] = make([]benchlib.Timing, 0, numTrials)
			}
		}
		results[on] = exp
	}
	return results
}

func measure(test func(int32)) *tResults {
	params := getParams()
	results := emptyResults()
	approx := benchlib.Time(0)
	for _, tp := range params {
		approx += roughEstimate * benchlib.Time((tp.parameter+tp.featureOn)*tp.iterations)
	}
	fmt.Println("# experiments will take approximately", approx, "at", time.Now())
	for _, tp := range params {
		runtime.GC()
		before := benchlib.GetSelfUsage()
		for iter := 0; iter < maxRepeatParam; iter++ {
			value := someWork(tp.parameter * workFactor)
			if tp.featureOn != 0 {
				test(value)
			}
		}
		after := benchlib.GetSelfUsage()
		diff := after.Sub(before).Div(maxRepeatParam)
		results[tp.featureOn][tp.parameter][tp.iterations] =
			append(results[tp.featureOn][tp.parameter][tp.iterations], diff)
	}
	return results
}

func computeConstants(test func(int32)) {
	fmt.Println("# work params", workParams)
	fmt.Println("# repeat params", repeatParams)
	var rough benchlib.Stats
	var work benchlib.Stats
	const large = 1e8 // this many repeats to rough calibrate work function

	for i := 0; i < roughTrials; i++ {
		rough1 := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				test(1<<31 - 1)
			}
		})
		rough.Update(rough1.T.Seconds() / float64(rough1.N))

		work1 := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				someWork(large)
			}
		})
		work.Update(work1.T.Seconds() / float64(work1.N) / large)
	}
	roughEstimate = benchlib.Time(rough.Mean())
	roughWork := work.Mean()
	workFactor = int(roughEstimate.Seconds() / roughWork)
	fmt.Printf("# udp send rough estimate %v\n# work timing %v\n", roughEstimate, roughWork)
}

func show(results *tResults) {
	for _, w := range workParams {
		for _, r := range repeatParams {
			off := benchlib.NewTimingStats(results[0][w][r])
			on := benchlib.NewTimingStats(results[1][w][r])

			onlow, _ := on.NormalConfidenceInterval()
			_, offhigh := off.NormalConfidenceInterval()
			fmt.Printf("# W/R=%v/%v MDIFF=%v SPREAD=%v\n",
				w, r, on.Mean().Sub(off.Mean()), onlow.Sub(offhigh))
		}
	}
	for _, r := range repeatParams {
		for _, w := range workParams {

			off := benchlib.NewTimingStats(results[0][w][r])
			on := benchlib.NewTimingStats(results[1][w][r])

			onlow, onhigh := on.NormalConfidenceInterval()
			offlow, offhigh := off.NormalConfidenceInterval()

			// Gross
			fmt.Printf("%v %v %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f %.9f\n",
				w, r, // 1 2
				on.Wall.Mean(), onlow.Wall.Seconds(), onhigh.Wall.Seconds(), // 3 4/5
				off.Wall.Mean(), offlow.Wall.Seconds(), offhigh.Wall.Seconds(), // 6 7/8

				on.User.Mean(), onlow.User.Seconds(), onhigh.User.Seconds(), // 9 10/11
				off.User.Mean(), offlow.User.Seconds(), offhigh.User.Seconds(), // 12 13/14

				on.Sys.Mean(), onlow.Sys.Seconds(), onhigh.Sys.Seconds(), // 15 16/17
				off.Sys.Mean(), offlow.Sys.Seconds(), offhigh.Sys.Seconds()) // 18 19/20

			// Use gnuplot e.g., for walltime
			// plot 'data' using 1:($4-$6):($5-$6) with filledcurves lt rgb "gray" title '95% confidence',  '' using 1:($3-$6) with lines title 'mean value'

			// For user+system
			// plot 'n=10.dat' using 1:($10+$16-$12-$18):($11+$17-$12-$18) with filledcurves lt rgb "#b0b0b0" title '95% confidence', '' using 1:($9+$15-$12-$18) with lines title 'mean value', '' using 1:($13+$19-$12-$18):($14+$20-$12-$18) with filledcurves lt rgb "#d0d0d0" title '95% confidence', '' using 1:($12+$18-$12-$18) with lines title 'mean value'
			// For user
			// plot 'data.dat' using 1:($13-$12):($14-$12) with filledcurves lt rgb "#d0d0d0" title '95% confidence', '' using 1:($12-$12) with lines title 'mean value', '' using 1:($10-$12):($11-$12) with filledcurves lt rgb "#b0b0b0" title '95% confidence', '' using 1:($9-$12) with lines title 'mean value'

			// For system
			// plot 'data.dat' using 1:($19-$18):($20-$18) with filledcurves lt rgb "#d0d0d0" title '95% confidence', '' using 1:($18-$18) with lines title 'mean value', '' using 1:($16-$18):($17-$18) with filledcurves lt rgb "#b0b0b0" title '95% confidence', '' using 1:($15-$18) with lines title 'mean value'

		}
		fmt.Println("")
	}
}

func main() {
	conn := connectUDP()
	test := func(id int32) { udpSend(id, conn) }

	computeConstants(test)
	results := measure(test)

	show(results)
}
