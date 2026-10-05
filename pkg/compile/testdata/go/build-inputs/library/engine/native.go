//go:build cgo

package engine

/*
#include "../native/detail.h"
int native_value();
*/
import "C"

func Value() string { return string(rune(C.native_value())) }
