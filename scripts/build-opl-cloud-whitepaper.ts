#!/usr/bin/env node

import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

type WhitepaperMetadata = {
  title: string;
  subtitle: string;
  publicationDate: string;
  owner: string;
  thesis: string;
};

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const sourceDir = path.join(repoRoot, 'docs', 'whitepapers');
const whitepaperDir = path.join(repoRoot, 'docs', 'site', 'latest', 'whitepapers');
const markdownPath = path.join(sourceDir, 'opl-cloud-whitepaper.md');
const generatedMarkdownPath = path.join(whitepaperDir, 'opl-cloud-whitepaper.md');
const htmlPath = path.join(whitepaperDir, 'opl-cloud-whitepaper.html');
const pdfPath = path.join(whitepaperDir, 'opl-cloud-whitepaper.pdf');
const verificationPath = path.join(repoRoot, 'docs', 'delivery', 'whitepapers', 'opl-cloud-whitepaper-verification.json');
const tempDir = path.join(repoRoot, 'tmp', 'pdfs', 'opl-cloud-whitepaper');
const tempMarkdownPath = path.join(tempDir, 'opl-cloud-whitepaper.pandoc.md');
const tempHeaderPath = path.join(tempDir, 'opl-cloud-whitepaper-header.tex');
const candidatePdfPath = path.join(tempDir, 'opl-cloud-whitepaper.candidate.pdf');

const forbiddenPatterns = [/sk-[A-Za-z0-9_-]+/, /OPENAI_API_KEY/, /CODEX_API_KEY/];

const requiredTerms = [
  'OPL Cloud 白皮书',
  'OPL Gateway',
  'OPL App',
  'OPL Workspace',
  'OPL Console',
  'OPL Fabric',
  'OPL Ledger',
  'OPL Connect',
  'OPL Compute',
  'OPL Environments',
  'OPL Agent Registry',
  '计划',
  '批准',
  '执行',
  '监控',
  '回收',
  '回执',
  'OPL Cloud 的五大设计原则',
  'OPL Cloud 的能力版图',
  '标准任务生命周期',
  '三类用户路径',
  '系统模型',
  '专业工作流',
  '五类信任机制',
  '本文边界',
  '结语',
];

function run(command: string, args: string[], options: { env?: NodeJS.ProcessEnv } = {}) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    encoding: 'utf8',
    stdio: 'pipe',
    env: { ...process.env, ...options.env },
  });
  if (result.status !== 0) {
    throw new Error([`Command failed: ${command} ${args.join(' ')}`, result.stdout, result.stderr].filter(Boolean).join('\n'));
  }
  return result;
}

function commandPath(command: string) {
  const result = spawnSync('which', [command], { encoding: 'utf8', stdio: 'pipe' });
  return result.status === 0 ? result.stdout.trim() : null;
}

function firstMatch(markdown: string, pattern: RegExp, label: string) {
  const match = pattern.exec(markdown);
  const value = match?.[1]?.trim();
  if (!value) throw new Error(`Whitepaper Markdown is missing ${label}.`);
  return value;
}

function parseMarkdownMetadata(markdown: string): WhitepaperMetadata {
  const title = firstMatch(markdown, /^#\s+(.+)$/m, 'top-level title');
  const subtitle = firstMatch(markdown, /^>\s+(.+)$/m, 'subtitle blockquote');
  const publicationDate = firstMatch(markdown, /^发布日期：(.+)$/m, 'publication date');
  const thesis = firstMatch(markdown, /^核心判断：(.+)$/m, 'core thesis');
  if (!/^\d{4}-\d{2}-\d{2}$/.test(publicationDate)) {
    throw new Error(`Whitepaper publication date must use YYYY-MM-DD, got ${publicationDate}.`);
  }
  for (const section of ['## 定位摘要', '## OPL Cloud 的五大设计原则', '## OPL Cloud 的能力版图', '## 标准任务生命周期', '## 三类用户路径', '## 系统模型', '## 五类信任机制', '## 本文边界', '## 结语']) {
    if (!markdown.includes(section)) throw new Error(`Whitepaper Markdown must include ${section}.`);
  }
  const sectionCount = (markdown.match(/^##\s+/gm) ?? []).length;
  if (sectionCount < 8) throw new Error(`Whitepaper Markdown must include at least eight second-level sections, got ${sectionCount}.`);
  return { title, subtitle, publicationDate, owner: 'One Person Lab', thesis };
}

function scanTextForSecrets(text: string) {
  const hits = forbiddenPatterns.filter((pattern) => pattern.test(text)).map(String);
  if (hits.length > 0) throw new Error(`Whitepaper text contains forbidden sensitive marker(s): ${hits.join(', ')}`);
}

function normalizePdfInlineCode(markdown: string) {
  return markdown.replace(/`([^`\n]+)`/g, '$1');
}

function stripMarkdownTitleBlock(markdown: string) {
  return markdown.replace(/^# .+\n\n> .+\n\n/, '').replace(/^## /gm, '# ').replace(/^### /gm, '## ');
}

function escapeLatexText(value: string) {
  return value
    .replace(/\\/g, '\\textbackslash{}')
    .replace(/([{}%$#&_])/g, '\\$1')
    .replace(/\^/g, '\\textasciicircum{}')
    .replace(/~/g, '\\textasciitilde{}');
}

function buildHeader() {
  return String.raw`
\usepackage{xcolor}
\usepackage{fancyhdr}
\usepackage{titlesec}
\usepackage{enumitem}
\usepackage{booktabs}
\usepackage{longtable}
\usepackage{array}
\definecolor{OPLTeal}{HTML}{0F766E}
\definecolor{OPLInk}{HTML}{101828}
\definecolor{OPLMuted}{HTML}{667085}
\definecolor{OPLLine}{HTML}{D0D5DD}
\setlength{\parindent}{0pt}
\setlength{\parskip}{6pt}
\setlist[itemize]{topsep=2pt,itemsep=2pt,leftmargin=18pt}
\titleformat{\section}{\Large\bfseries\color{OPLTeal}}{\thesection}{0.7em}{}
\titleformat{\subsection}{\large\bfseries\color{OPLInk}}{\thesubsection}{0.7em}{}
\titleformat{\subsubsection}{\normalsize\bfseries\color{OPLInk}}{\thesubsubsection}{0.7em}{}
\pagestyle{fancy}
\fancyhf{}
\lhead{\small\color{OPLMuted}One Person Lab}
\rhead{\small\color{OPLMuted}OPL Cloud Whitepaper}
\cfoot{\small\thepage}
\renewcommand{\headrulewidth}{0.3pt}
\renewcommand{\headrule}{\hbox to\headwidth{\color{OPLLine}\leaders\hrule height \headrulewidth\hfill}}
`;
}

function buildPdfMarkdown(metadata: WhitepaperMetadata, markdown: string) {
  const cover = [
    '\\begin{titlepage}',
    '\\thispagestyle{empty}',
    '\\vspace*{26mm}',
    '{\\color{OPLTeal}\\Large One Person Lab\\par}',
    '\\vspace{18mm}',
    `{\\Huge\\bfseries ${escapeLatexText(metadata.title)}\\par}`,
    '\\vspace{8mm}',
    `{\\LARGE ${escapeLatexText(metadata.subtitle)}\\par}`,
    '\\vspace{18mm}',
    `{\\large ${escapeLatexText(metadata.thesis)}\\par}`,
    '\\vspace{10mm}',
    '{\\large OPL Gateway / OPL Workspace / OPL Console / OPL Fabric / OPL Ledger\\par}',
    '\\vfill',
    `{\\large ${metadata.publicationDate}\\par}`,
    '\\vspace{4mm}',
    '{\\small Public whitepaper\\par}',
    '\\end{titlepage}',
    '\\newpage',
    '\\tableofcontents',
    '\\newpage',
    '',
  ].join('\n');
  return `${cover}${stripMarkdownTitleBlock(normalizePdfInlineCode(markdown))}`;
}

function buildPdf(metadata: WhitepaperMetadata, markdown: string, outputPath: string) {
  fs.mkdirSync(tempDir, { recursive: true });
  fs.mkdirSync(whitepaperDir, { recursive: true });
  fs.writeFileSync(tempHeaderPath, buildHeader(), 'utf8');
  fs.writeFileSync(tempMarkdownPath, buildPdfMarkdown(metadata, markdown), 'utf8');
  const font = process.env.OPL_WHITEPAPER_PDF_FONT || 'Noto Sans CJK SC';
  const sourceDateEpoch = String(Math.floor(new Date(`${metadata.publicationDate}T00:00:00Z`).getTime() / 1000));
  run('pandoc', [
    tempMarkdownPath,
    '--standalone',
    '--pdf-engine=xelatex',
    '--number-sections',
    '--metadata', `title-meta=${metadata.title}`,
    '--metadata', `author-meta=${metadata.owner}`,
    '--metadata', 'lang=zh-CN',
    '--include-in-header', tempHeaderPath,
    '-V', `mainfont=${font}`,
    '-V', `CJKmainfont=${font}`,
    '-V', 'geometry:margin=18mm',
    '-V', 'colorlinks=true',
    '-V', 'linkcolor=OPLTeal',
    '-V', 'urlcolor=OPLTeal',
    '-o', outputPath,
  ], { env: { SOURCE_DATE_EPOCH: sourceDateEpoch } });
}

function buildHtml(metadata: WhitepaperMetadata) {
  fs.mkdirSync(whitepaperDir, { recursive: true });
  run('pandoc', [
    markdownPath,
    '--standalone',
    '--metadata', `title=${metadata.title}`,
    '--metadata', 'lang=zh-CN',
    '-o', htmlPath,
  ]);
}

function parsePdfInfo(pdfFile: string) {
  const result = run('pdfinfo', [pdfFile]);
  const pages = Number(result.stdout.match(/^Pages:\s+(\d+)/m)?.[1] ?? 0);
  const size = result.stdout.match(/^Page size:\s+([\d.]+)\s+x\s+([\d.]+)\s+pts/m);
  return {
    raw: result.stdout,
    pages,
    page_size_pts: {
      width: Number(size?.[1] ?? 0),
      height: Number(size?.[2] ?? 0),
    },
  };
}

function renderPdf(pdfFile: string) {
  const renderDir = path.join(tempDir, 'rendered');
  fs.rmSync(renderDir, { recursive: true, force: true });
  fs.mkdirSync(renderDir, { recursive: true });
  run('pdftoppm', ['-png', '-r', '120', pdfFile, path.join(renderDir, 'page')]);
  return { renderDir, pages: fs.readdirSync(renderDir).filter((name) => name.endsWith('.png')).sort() };
}

function extractPdfText(pdfFile: string) {
  return run('pdftotext', [pdfFile, '-']).stdout.replace(/[\u2010-\u2015]/g, '-');
}

function fileSha1(filePath: string) {
  return crypto.createHash('sha1').update(fs.readFileSync(filePath)).digest('hex');
}

function parseMarkdownLinks(markdown: string) {
  return [...markdown.matchAll(/- \[([^\]]+)\]\(([^)]+)\)：(.+)/g)].map((match) => ({
    label: match[1],
    url: match[2],
    note: match[3],
  }));
}

function relativeToRepo(filePath: string) {
  return path.relative(repoRoot, filePath);
}

function main() {
  fs.mkdirSync(whitepaperDir, { recursive: true });
  const markdown = fs.readFileSync(markdownPath, 'utf8');
  const metadata = parseMarkdownMetadata(markdown);
  scanTextForSecrets(markdown);
  buildHtml(metadata);
  buildPdf(metadata, markdown, candidatePdfPath);
  fs.copyFileSync(candidatePdfPath, pdfPath);
  fs.rmSync(candidatePdfPath, { force: true });

  const render = renderPdf(pdfPath);
  const info = parsePdfInfo(pdfPath);
  if (info.pages < 8) throw new Error(`Expected whitepaper PDF to have at least 8 pages, got ${info.pages}.`);
  if (info.page_size_pts.height <= info.page_size_pts.width) {
    throw new Error(`Expected portrait PDF, got ${info.page_size_pts.width}x${info.page_size_pts.height} pts.`);
  }
  const text = extractPdfText(pdfPath);
  const missingTerms = requiredTerms.filter((term) => !text.includes(term));
  if (missingTerms.length > 0) throw new Error(`Generated PDF text is missing required terms: ${missingTerms.join(', ')}`);

  const renderedPageHashes = render.pages.map((page) => ({
    page,
    sha1: fileSha1(path.join(render.renderDir, page)),
  }));
  const verification = {
    status: 'opl_cloud_whitepaper_ready',
    generated_at: `${metadata.publicationDate}T00:00:00.000Z`,
    source_markdown: relativeToRepo(markdownPath),
    generated_markdown: relativeToRepo(generatedMarkdownPath),
    generated_html: relativeToRepo(htmlPath),
    generated_pdf: relativeToRepo(pdfPath),
    temp_markdown: relativeToRepo(tempMarkdownPath),
    rendered_dir: relativeToRepo(render.renderDir),
    rendered_pages: render.pages.length,
    rendered_page_hashes: renderedPageHashes,
    pdf_pages: info.pages,
    pdf_page_size_pts: info.page_size_pts,
    required_terms: requiredTerms,
    required_terms_status: 'present',
    tools: {
      pandoc: commandPath('pandoc'),
      xelatex: commandPath('xelatex'),
      pdfinfo: commandPath('pdfinfo'),
      pdftoppm: commandPath('pdftoppm'),
      pdftotext: commandPath('pdftotext'),
    },
    references: parseMarkdownLinks(markdown),
  };
  fs.copyFileSync(markdownPath, generatedMarkdownPath);
  fs.mkdirSync(path.dirname(verificationPath), { recursive: true });
  fs.writeFileSync(verificationPath, `${JSON.stringify(verification, null, 2)}\n`, 'utf8');
  console.log(JSON.stringify(verification, null, 2));
}

main();
