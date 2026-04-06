package vm

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
)

// ── std.csv — CSV encode/decode ──

func (vm *VM) builtinCsvDec() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	records, csvErr := csv.NewReader(strings.NewReader(s)).ReadAll()
	if csvErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.dec: %v", csvErr))
	}
	vm.push(csvRecordsToValue(records))
	return nil
}

func (vm *VM) builtinCsvEnc() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	records := valueToCsvRecords(list)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if writeErr := w.WriteAll(records); writeErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.enc: %v", writeErr))
	}
	vm.push(ValStr(buf.String()))
	return nil
}

func (vm *VM) builtinCsvDecf() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.decf: %v", readErr))
	}
	records, csvErr := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if csvErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.decf: %v", csvErr))
	}
	vm.push(csvRecordsToValue(records))
	return nil
}

func (vm *VM) builtinCsvEncf() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	records := valueToCsvRecords(list)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if writeErr := w.WriteAll(records); writeErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.encf: %v", writeErr))
	}
	if writeErr := os.WriteFile(path, buf.Bytes(), 0644); writeErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.encf: %v", writeErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinCsvHdr() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return vm.trap("E004", "csv.hdr: empty CSV data")
	}
	vm.push(list[0])
	return nil
}

func (vm *VM) builtinCsvRows() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		vm.push(ValList(nil))
		return nil
	}
	result := make([]Value, len(list)-1)
	copy(result, list[1:])
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinCsvMaps() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) < 1 {
		vm.push(ValList(nil))
		return nil
	}
	// Extract header names
	hdrRow := list[0]
	if hdrRow.Tag != TagHEAP || hdrRow.Heap.Kind != KindList {
		return vm.trap("E002", "csv.maps: expected list of lists")
	}
	headers := make([]string, len(hdrRow.Heap.Items))
	for i, h := range hdrRow.Heap.Items {
		if h.Tag == TagSTR {
			headers[i] = h.StrVal
		} else {
			headers[i] = fmt.Sprintf("col%d", i)
		}
	}
	var result []Value
	for _, row := range list[1:] {
		if row.Tag != TagHEAP || row.Heap.Kind != KindList {
			continue
		}
		fields := make(map[string]Value)
		order := make([]string, 0, len(headers))
		for i, hdr := range headers {
			if i < len(row.Heap.Items) {
				fields[hdr] = row.Heap.Items[i]
			} else {
				fields[hdr] = ValStr("")
			}
			order = append(order, hdr)
		}
		result = append(result, ValRecord(fields, order))
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinCsvOpts() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	optsVal, _ := vm.pop()
	delim := ','
	if optsVal.Tag == TagHEAP && optsVal.Heap.Kind == KindRecord {
		if d, ok := optsVal.Heap.Fields["delim"]; ok && d.Tag == TagSTR && len(d.StrVal) > 0 {
			delim = rune(d.StrVal[0])
		}
	}
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = delim
	records, csvErr := r.ReadAll()
	if csvErr != nil {
		return vm.trap("E900", fmt.Sprintf("csv.opts: %v", csvErr))
	}
	vm.push(csvRecordsToValue(records))
	return nil
}

// helpers

func csvRecordsToValue(records [][]string) Value {
	rows := make([]Value, len(records))
	for i, rec := range records {
		cells := make([]Value, len(rec))
		for j, cell := range rec {
			cells[j] = ValStr(cell)
		}
		rows[i] = ValList(cells)
	}
	return ValList(rows)
}

func valueToCsvRecords(list []Value) [][]string {
	records := make([][]string, len(list))
	for i, row := range list {
		if row.Tag == TagHEAP && row.Heap.Kind == KindList {
			cells := make([]string, len(row.Heap.Items))
			for j, cell := range row.Heap.Items {
				if cell.Tag == TagSTR {
					cells[j] = cell.StrVal
				} else {
					cells[j] = valShow(cell)
				}
			}
			records[i] = cells
		}
	}
	return records
}
