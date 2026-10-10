#!/usr/bin/env node
/**
 * generate-favicon.mjs — 生成 favicon.ico（16x16 + 32x32，8bpp 索引色）
 *
 * 为什么需要这个脚本：.ico 是二进制格式，文件头/调色板中必须含 0x00 字节，
 * 而本仓库的 AI 会话环境无法传输原始 NUL 字节，所以由你在本机用 Node 生成。
 *
 * 用法（仓库根目录）:
 *   node generate-favicon.mjs
 *
 * 依赖: 仅 Node 内置模块。
 * 生成后会:
 *   1. 覆盖 frontend/public/favicon.ico（写出真实的 16+32 双尺寸 .ico）
 *   2. 若 frontend/index.html 还没有指向 favicon.ico 的 <link>，自动补上
 *      （已有 /favicon.svg 的 link，两者共存，现代浏览器优先用 SVG）
 */

import { writeFileSync, readFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(fileURLToPath(import.meta.url));
const icoPath = join(root, 'frontend', 'public', 'favicon.ico');
const htmlPath = join(root, 'frontend', 'index.html');

// ---- 调色板 (BGRA, 51 色: 0-47 占位色, 48=背景, 49=卡片, 50=墨色) ----
const FILLER = [0x68, 0x75, 0x2f, 0x00]; // #2f7568
const BG = [0x68, 0x75, 0x0a, 0x00]; // #0a7568
const CARD = [0xff, 0xff, 0xff, 0x00]; // #ffffff
const INK = [0x56, 0x63, 0x08, 0x00]; // #086356
const IDX_BG = 48;
const IDX_CARD = 49;
const IDX_INK = 50;
const NC = 51; // biClrUsed

/** 绘制 s×s 索引图，与 favicon.svg 同一设计 */
function draw(s) {
  const g = new Uint8Array(s * s);
  g.fill(IDX_BG);
  const x0 = Math.round(s * 0.1875),
    x1 = Math.round(s * 0.8125); // 3..13 @16
  const y0 = Math.round(s * 0.125),
    y1 = Math.round(s * 0.875); // 2..14 @16
  const cw = x1 - x0,
    ch = y1 - y0;
  for (let y = y0; y < y1; y++)
    for (let x = x0; x < x1; x++) g[y * s + x] = IDX_CARD;
  const hdr = Math.max(1, Math.round(ch * 0.15));
  for (let y = y0; y < y0 + hdr; y++)
    for (let x = x0; x < x1; x++) g[y * s + x] = IDX_INK;
  const top = y0 + hdr,
    usable = y1 - top;
  const t = Math.max(1, Math.round(ch * 0.12));
  const space = usable - 3 * t;
  const base = Math.floor(space / 4),
    rem = space % 4;
  const gaps = [];
  for (let j = 0; j < 4; j++) gaps.push(base + (j < rem ? 1 : 0));
  let yc = top + gaps[0];
  const fr = [0.8, 0.8, 0.48];
  for (let i = 0; i < 3; i++) {
    const w = Math.round(cw * fr[i]);
    for (let y = yc; y < yc + t; y++)
      for (let x = x0; x < x0 + w; x++) g[y * s + x] = IDX_INK;
    yc += t + gaps[i + 1];
  }
  if (yc !== y1) throw new Error(`row layout mismatch: ${yc} != ${y1}`);
  return g;
}

const le = (n) => [n & 0xff, (n >>> 8) & 0xff, (n >>> 16) & 0xff, (n >>> 24) & 0xff];
const le16 = (n) => [n & 0xff, (n >>> 8) & 0xff];

/** 单张图: DIB 头 + 调色板 + XOR(8bpp) + AND 掩码 */
function buildImage(s) {
  const g = draw(s);
  // s 为 16/32，都是 4 的倍数，XOR 行无填充
  const maskRow = ((s + 31) >> 5) << 2;
  const header = [
    ...le(40), // biSize
    ...le(s), // biWidth
    ...le(s * 2), // biHeight = XOR + AND
    ...le16(1), // biPlanes
    ...le16(8), // biBitCount
    ...le(0), // biCompression = BI_RGB
    ...le(s * s + maskRow * s), // biSizeImage
    ...le(0),
    ...le(0),
    ...le(NC), // biClrUsed
    ...le(0), // biClrImportant
  ];
  const palette = [];
  for (let i = 0; i < NC; i++) {
    let e = FILLER;
    if (i === IDX_BG) e = BG;
    else if (i === IDX_CARD) e = CARD;
    else if (i === IDX_INK) e = INK;
    palette.push(...e);
  }
  const xor = [];
  for (let y = s - 1; y >= 0; y--) {
    for (let x = 0; x < s; x++) xor.push(g[y * s + x]);
    // 无需行填充 (s 为 4 的倍数)
  }
  const and = [];
  for (let y = s - 1; y >= 0; y--) {
    const row = [];
    for (let x = 0; x < s; x++) {
      const v = g[y * s + x] === IDX_BG ? 1 : 0;
      const bi = x >> 3;
      if (row[bi] === undefined) row[bi] = 0;
      if (v) row[bi] |= 0x80 >> (x & 7);
    }
    while (row.length < maskRow) row.push(0);
    and.push(...row);
  }
  return { bytes: [...header, ...palette, ...xor, ...and], width: s };
}

const images = [buildImage(16), buildImage(32)];

// ---- ICONDIR + ICONDIRENTRY ----
const dir = [0, 0, 1, 0, ...le16(images.length)];
let offset = 6 + 16 * images.length;
const entries = [];
for (const img of images) {
  entries.push([
    img.width === 256 ? 0 : img.width, // bWidth
    img.width === 256 ? 0 : img.width, // bHeight
    0, // bColorCount
    0, // bReserved
    ...le16(1), // wPlanes
    ...le16(32), // wBitCount
    ...le(img.bytes.length), // dwBytesInRes
    ...le(offset), // dwImageOffset
  ]);
  offset += img.bytes.length;
}

const all = [...dir, ...entries.flat(), ...images.flatMap((i) => i.bytes)];

// ---- 校验 ----
const actualSizes = images.map((i) => i.bytes.length);
const actualOffsets = [];
{
  let o = 6 + 16 * images.length;
  for (const img of images) {
    actualOffsets.push(o);
    o += img.bytes.length;
  }
}
if (all.length !== 1998) throw new Error(`bad length: ${all.length}`);
if (JSON.stringify(actualOffsets) !== '[38,602]') throw new Error(`bad offsets: ${actualOffsets}`);
if (JSON.stringify(actualSizes) !== '[564,1396]') throw new Error(`bad sizes: ${actualSizes}`);
if (all.some((b) => b < 0 || b > 255 || !Number.isInteger(b))) throw new Error('bad byte value');
if (all.length !== offset) throw new Error('offset/length mismatch');

writeFileSync(icoPath, Buffer.from(all));
console.log(`✓ 已写入 ${icoPath} (${all.length} bytes, 16x16 + 32x32, 8bpp 索引色)`);

// ---- 确保 index.html 有 favicon.ico 的 <link> ----
if (existsSync(htmlPath)) {
  let html = readFileSync(htmlPath, 'utf8');
  if (!html.includes('favicon.ico')) {
    const patched = html.replace(
      /([ \t]*<title>[^<]*<\/title>[^\r\n]*\r?\n)/,
      '$1    <link rel="icon" href="/favicon.ico" sizes="32x32" />\n',
    );
    if (patched === html) {
      console.warn('! 未能在 frontend/index.html 中找到 <title>，请手动添加：');
      console.warn('  <link rel="icon" href="/favicon.ico" sizes="32x32" />');
    } else {
      writeFileSync(htmlPath, patched);
      console.log('✓ 已在 frontend/index.html 中加入 favicon.ico <link>');
    }
  } else {
    console.log('• frontend/index.html 已含 favicon.ico <link>，跳过');
  }
} else {
  console.warn(`! 未找到 ${htmlPath}`);
}
