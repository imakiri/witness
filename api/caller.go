package witness

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var callDepth int

//var atSize int

func SetCallDepth(i int) {
	callDepth = i
	pcPool = new(sync.Pool)
	pcPool.New = func() any {
		return make([]uintptr, callDepth)
	}
}

//func SetAtSize(i int) {
//	atSize = i
//	atPool = new(sync.Pool)
//	atPool.New = func() any {
//		return make([]byte, atSize)
//	}
//}

func init() {
	SetCallDepth(16)
	//SetAtSize(128)
}

var pcPool *sync.Pool

//var atPool *sync.Pool

// caller TODO there might be a bug with at_line
func caller(skip, extra int) (string, string) {
	var details *runtime.Func
	var pc = pcPool.Get().([]uintptr)
	var size = runtime.Callers(skip+1, pc)
	var i int
	for i = 0; i < size; i++ {
		details = runtime.FuncForPC(pc[i])
		if details == nil {
			return "", ""
		}
		//fmt.Println(details.Name())
		if !strings.Contains(details.Name(), ".func") {
			break
		}
	}
	if !(i+extra < size) {
		return "", ""
	}
	details = runtime.FuncForPC(pc[i+extra])
	if details == nil {
		return "", ""
	}

	//fmt.Println("extra", details.Name(), "pc", pc[i+extra])
	var atFile, atLine = details.FileLine(pc[i+extra])
	var c strings.Builder
	c.WriteString(atFile)
	c.WriteRune(':')
	c.WriteString(strconv.FormatInt(int64(atLine), 10))
	return details.Name(), c.String()
}
