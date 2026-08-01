// xlsx.go：用 Go 标准库（archive/zip + encoding/xml）生成最小 .xlsx 文件，
// 用于导出文件统计差异（新增/删除文件列表，两个 sheet）。无第三方依赖。
// 代码注释使用中文
package api

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// xlsx 文件内部固定内容（最小可用 OOXML 结构，单元格用 inlineStr 内联字符串）。

const xlsxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/worksheets/sheet2.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`

const xlsxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const xlsxWorkbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>
</Relationships>`

// xlsxWorkbookXML 生成 workbook.xml（sheet 名称：新增文件列表 / 删除文件列表）。
func xlsxWorkbookXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="新增文件列表" sheetId="1" r:id="rId1"/>
    <sheet name="删除文件列表" sheetId="2" r:id="rId2"/>
  </sheets>
</workbook>`
}

// xlsxSheetXML 生成单个工作表：第一行表头"文件路径"，其后逐行写入路径（按传入顺序）。
func xlsxSheetXML(paths []string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>`)
	// 表头行
	sb.WriteString(`    <row r="1"><c r="A1" t="inlineStr"><is><t>文件路径</t></is></c></row>`)
	for i, p := range paths {
		row := i + 2
		// 内联字符串，XML 转义由 xml.EscapeText 完成
		var t bytes.Buffer
		_ = xml.EscapeText(&t, []byte(p))
		sb.WriteString(fmt.Sprintf(`    <row r="%d"><c r="A%d" t="inlineStr"><is><t>%s</t></is></c></row>`, row, row, t.String()))
	}
	sb.WriteString(`  </sheetData>
</worksheet>`)
	return sb.String()
}

// exportDiffXLSX 生成包含两个 sheet（新增/删除文件列表）的 xlsx 字节流。
// added/removed 为绝对路径列表（正斜杠），按调用方传入顺序写入。
func exportDiffXLSX(added, removed []string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entries := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", xlsxContentTypes},
		{"_rels/.rels", xlsxRootRels},
		{"xl/workbook.xml", xlsxWorkbookXML()},
		{"xl/_rels/workbook.xml.rels", xlsxWorkbookRels},
		{"xl/worksheets/sheet1.xml", xlsxSheetXML(added)},
		{"xl/worksheets/sheet2.xml", xlsxSheetXML(removed)},
	}
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			return nil, fmt.Errorf("创建 xlsx 内部文件 %s: %w", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			return nil, fmt.Errorf("写入 xlsx 内部文件 %s: %w", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("关闭 xlsx 压缩流: %w", err)
	}
	return buf.Bytes(), nil
}
