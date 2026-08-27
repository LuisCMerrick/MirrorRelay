#!/usr/bin/env node

import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const failures = [];

function fail(message) {
  failures.push(message);
}

function markdownFilesUnder(relativeDirectory) {
  const directory = path.join(root, relativeDirectory);
  if (!fs.existsSync(directory)) return [];
  const files = [];
  const visit = current => {
    for (const entry of fs.readdirSync(current, {withFileTypes: true})) {
      const target = path.join(current, entry.name);
      if (entry.isDirectory()) visit(target);
      else if (entry.isFile() && entry.name.endsWith('.md')) files.push(target);
    }
  };
  visit(directory);
  return files;
}

const markdownFiles = [
  'README.md',
  'README.zh-CN.md',
  'CHANGELOG.md',
  'CONTRIBUTING.md',
  'ROADMAP.md',
  ...markdownFilesUnder('docs').map(file => path.relative(root, file)),
  ...markdownFilesUnder('.github').map(file => path.relative(root, file)),
  ...markdownFilesUnder('nginx').map(file => path.relative(root, file))
].map(file => path.join(root, file));

function structure(file) {
  const lines = fs.readFileSync(file, 'utf8').split(/\r?\n/);
  const headings = [];
  const fences = [];
  let fence = null;
  for (const line of lines) {
    const match = line.match(/^\s*(```+|~~~+)\s*([^\s`]*)/);
    if (match) {
      const marker = match[1][0];
      if (fence === null) {
        fence = marker;
        fences.push(match[2] || 'plain');
      } else if (marker === fence) {
        fence = null;
      }
      continue;
    }
    if (fence === null) {
      const heading = line.match(/^(#{1,6})\s+/);
      if (heading) headings.push(heading[1].length);
    }
  }
  if (fence !== null) fail(`${path.relative(root, file)} has an unclosed code fence`);
  return {headings, fences};
}

function comparePair(english, chinese) {
  const enPath = path.join(root, english);
  const zhPath = path.join(root, chinese);
  if (!fs.existsSync(zhPath)) {
    fail(`${english} is missing paired translation ${chinese}`);
    return;
  }
  const en = structure(enPath);
  const zh = structure(zhPath);
  if (JSON.stringify(en.headings) !== JSON.stringify(zh.headings)) {
    fail(`${english} and ${chinese} have different heading-level sequences`);
  }
  if (JSON.stringify(en.fences) !== JSON.stringify(zh.fences)) {
    fail(`${english} and ${chinese} have different fenced-code language sequences`);
  }
}

comparePair('README.md', 'README.zh-CN.md');
for (const englishPath of markdownFilesUnder('docs').filter(file => !file.endsWith('.zh-CN.md'))) {
  const relative = path.relative(root, englishPath);
  comparePair(relative, relative.replace(/\.md$/, '.zh-CN.md'));
}

function validateLocalLinks(file) {
  const content = fs.readFileSync(file, 'utf8');
  const linkPattern = /!?\[[^\]]*\]\((<[^>]+>|[^)\s]+)(?:\s+["'][^)]*["'])?\)/g;
  for (const match of content.matchAll(linkPattern)) {
    let target = match[1].replace(/^<|>$/g, '');
    if (/^(?:[a-z][a-z0-9+.-]*:|#|\/)/i.test(target)) continue;
    target = target.split('#', 1)[0].split('?', 1)[0];
    if (!target) continue;
    try {
      target = decodeURIComponent(target);
    } catch {
      fail(`${path.relative(root, file)} has an invalid encoded link target: ${match[1]}`);
      continue;
    }
    const resolved = path.resolve(path.dirname(file), target);
    if (!resolved.startsWith(root + path.sep) && resolved !== root) {
      fail(`${path.relative(root, file)} links outside the repository: ${match[1]}`);
    } else if (!fs.existsSync(resolved)) {
      fail(`${path.relative(root, file)} has a missing local link target: ${match[1]}`);
    }
  }
}

for (const file of markdownFiles) validateLocalLinks(file);

const activeDocs = [
  path.join(root, 'README.md'),
  path.join(root, 'README.zh-CN.md'),
  ...markdownFilesUnder('docs')
];
const prohibitedTerms = [
  'Autonomous Upstream Nginx',
  '└── Upstream Nginx Candidate Generator',
  '受管上游 Nginx',
  '受管 Nginx'
];
for (const file of activeDocs) {
  const content = fs.readFileSync(file, 'utf8');
  for (const term of prohibitedTerms) {
    if (content.includes(term)) fail(`${path.relative(root, file)} contains obsolete product terminology: ${term}`);
  }
}

if (failures.length > 0) {
  for (const failure of failures) process.stderr.write(`documentation verification: ${failure}\n`);
  process.exit(1);
}

process.stdout.write(`Verified ${markdownFiles.length} Markdown files and all bilingual pairs.\n`);
