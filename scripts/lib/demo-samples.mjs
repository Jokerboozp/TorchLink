import { writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */

// Small standard OOXML workbook, stored ZIP entries; no office/npm dependency.
function zip(files) { /* 定义 zip 函数。 */
  const local=[], central=[];let offset=0 /* 声明 local。 */
  for(const [path,text] of Object.entries(files)) { /* 循环处理当前数据。 */
    const name=Buffer.from(path), data=Buffer.from(text) /* 声明 name。 */
    let crc=0xffffffff /* 声明 crc。 */
    for(const byte of data) { crc^=byte;for(let bit=0;bit<8;bit++) crc=(crc>>>1)^((crc&1)?0xedb88320:0) } /* 循环处理当前数据。 */
    crc=(crc^0xffffffff)>>>0 /* 更新 crc 的值。 */
    const header=Buffer.alloc(30);header.writeUInt32LE(0x04034b50);header.writeUInt16LE(20,4);header.writeUInt16LE(0x21,12);header.writeUInt32LE(crc,14);header.writeUInt32LE(data.length,18);header.writeUInt32LE(data.length,22);header.writeUInt16LE(name.length,26) /* 声明 header。 */
    const entry=Buffer.alloc(46);entry.writeUInt32LE(0x02014b50);entry.writeUInt16LE(20,4);entry.writeUInt16LE(20,6);entry.writeUInt16LE(0x21,14);entry.writeUInt32LE(crc,16);entry.writeUInt32LE(data.length,20);entry.writeUInt32LE(data.length,24);entry.writeUInt16LE(name.length,28);entry.writeUInt32LE(offset,42) /* 声明 entry。 */
    local.push(header,name,data);central.push(entry,name);offset+=header.length+name.length+data.length /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  const directory=Buffer.concat(central),end=Buffer.alloc(22);end.writeUInt32LE(0x06054b50);end.writeUInt16LE(Object.keys(files).length,8);end.writeUInt16LE(Object.keys(files).length,10);end.writeUInt32LE(directory.length,12);end.writeUInt32LE(offset,16) /* 声明 directory。 */
  return Buffer.concat([...local,directory,end]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export async function writeDemoSamples(dir) { /* 执行当前语句并推进处理流程。 */
  const rows=[['name','identifier','functionCode','address','addressNotation','dataType','scale','unit'],['温度','temperature','3','0','zero_based','uint16','0.1','℃'],['湿度','humidity','3','1','zero_based','uint16','0.1','%']] /* 声明 rows。 */
  const sheet=rows.map((row,i)=>`<row r="${i+1}">${row.map((cell,j)=>`<c r="${String.fromCharCode(65+j)}${i+1}" t="inlineStr"><is><t>${cell}</t></is></c>`).join('')}</row>`).join('') /* 声明 sheet。 */
  const files={ /* 声明 files。 */
    '[Content_Types].xml':'<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>', /* 执行当前语句并推进处理流程。 */
    '_rels/.rels':'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>', /* 执行当前语句并推进处理流程。 */
    'xl/workbook.xml':'<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="演示点表" sheetId="1" r:id="rId1"/></sheets></workbook>', /* 执行当前语句并推进处理流程。 */
    'xl/_rels/workbook.xml.rels':'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>', /* 执行当前语句并推进处理流程。 */
    'xl/worksheets/sheet1.xml':`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>${sheet}</sheetData></worksheet>` /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  await writeFile(join(dir,'演示点表.xlsx'),zip(files)) /* 等待异步操作完成。 */
  await writeFile(join(dir,'演示点表.csv'),rows.map(row=>row.join(',')).join('\n')) /* 等待异步操作完成。 */
  await writeFile(join(dir,'演示报文.json'),JSON.stringify({data:{temperature:25.5,humidity:48}},null,2)) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
