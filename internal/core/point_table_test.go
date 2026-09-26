package core

import (
	"strings"
	"testing"
)

func TestParseAndCompileModbusPointTable(t *testing.T) {
	csv := "标识,名称,功能码,地址,数据类型,倍率,轮询周期\n" +
		"temperature,温度,03,40001,int16,0.1,10\n" +
		"pressure,压力,03,40002,uint16,1,10\n" +
		"smoke,烟感,01,0,bool,1,5\n"
	table, warnings, err := ParseModbusPointTable("points.csv", []byte(csv), 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(table.Points) != 3 {
		t.Fatalf("unexpected import result: points=%d warnings=%v", len(table.Points), warnings)
	}
	if table.Points[0].Address != 0 || table.Points[0].FunctionCode != 3 || table.Points[0].Scale != 0.1 {
		t.Fatalf("first point was not normalized: %+v", table.Points[0])
	}
	blocks, err := CompileModbusReadBlocks(table.Points)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want two function/interval blocks, got %+v", blocks)
	}
}

func TestPointTableRejectsAmbiguousAddress(t *testing.T) {
	_, _, err := ParseModbusPointTable("points.csv", []byte("名称,地址,数据类型\n温度,100,int16\n"), 10)
	if err == nil || !strings.Contains(err.Error(), "functionCode is required") {
		t.Fatalf("expected explicit function-code error, got %v", err)
	}
}
