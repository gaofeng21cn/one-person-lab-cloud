// A minimal, safe Markdown renderer for one Agent answer.
//
// The renderer builds DOM nodes and never assigns innerHTML, so an answer can
// never inject markup, script or styles into this page. The supported subset
// is the one an Agent answer actually uses: headings, bold, italic, inline
// code, fenced code blocks, ordered and unordered lists, block quotes, links
// and paragraphs.

const SAFE_LINK = /^https?:\/\//i;

function element(tag, text) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  return node;
}

function textNode(text) {
  return document.createTextNode(text);
}

// inlineNodes splits one line into its text, bold, italic, code and link nodes.
function inlineNodes(text) {
  const nodes = [];
  const pattern = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\n]+\*)|(\[[^\]\n]+\]\([^)\s]+\))/g;
  let index = 0;
  let match;
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > index) nodes.push(textNode(text.slice(index, match.index)));
    const token = match[0];
    if (token.startsWith("`")) {
      nodes.push(element("code", token.slice(1, -1)));
    } else if (token.startsWith("**")) {
      nodes.push(element("strong", token.slice(2, -2)));
    } else if (token.startsWith("[")) {
      const split = token.indexOf("](");
      const label = token.slice(1, split);
      const href = token.slice(split + 2, -1);
      // A link the Agent did not earn stays plain text: only http(s) targets
      // become links, and they open in a new context without the opener.
      if (SAFE_LINK.test(href)) {
        const link = element("a", label);
        link.href = href;
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        nodes.push(link);
      } else {
        nodes.push(textNode(token));
      }
    } else {
      nodes.push(element("em", token.slice(1, -1)));
    }
    index = match.index + token.length;
  }
  if (index < text.length) nodes.push(textNode(text.slice(index)));
  return nodes;
}

function startsBlock(line) {
  return /^```/.test(line) || /^#{1,6}\s+/.test(line) || /^>\s?/.test(line) ||
    /^[-*]\s+/.test(line) || /^\d+[.)]\s+/.test(line);
}

/** renderMarkdown turns one answer into a document fragment of DOM nodes. */
export function renderMarkdown(source) {
  const fragment = document.createDocumentFragment();
  const lines = String(source ?? "").split(/\r?\n/);
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) {
      index += 1;
      continue;
    }
    const fence = line.match(/^```(.*)$/);
    if (fence) {
      const codeLines = [];
      index += 1;
      while (index < lines.length && !/^```/.test(lines[index])) {
        codeLines.push(lines[index]);
        index += 1;
      }
      index += 1;
      const pre = element("pre");
      pre.append(element("code", codeLines.join("\n")));
      fragment.append(pre);
      continue;
    }
    const heading = line.match(/^(#{1,6})\s+(.*)$/);
    if (heading) {
      const node = element(`h${heading[1].length}`);
      node.append(...inlineNodes(heading[2]));
      fragment.append(node);
      index += 1;
      continue;
    }
    const quote = line.match(/^>\s?(.*)$/);
    if (quote) {
      const block = element("blockquote");
      block.append(...inlineNodes(quote[1]));
      fragment.append(block);
      index += 1;
      continue;
    }
    const bullet = line.match(/^[-*]\s+(.*)$/);
    const numbered = line.match(/^\d+[.)]\s+(.*)$/);
    if (bullet || numbered) {
      const list = element(bullet ? "ul" : "ol");
      while (index < lines.length) {
        const item = bullet ? lines[index].match(/^[-*]\s+(.*)$/) : lines[index].match(/^\d+[.)]\s+(.*)$/);
        if (!item) break;
        const entry = element("li");
        entry.append(...inlineNodes(item[1]));
        list.append(entry);
        index += 1;
      }
      fragment.append(list);
      continue;
    }
    const paragraphLines = [];
    while (index < lines.length && lines[index].trim() && !startsBlock(lines[index])) {
      paragraphLines.push(lines[index]);
      index += 1;
    }
    const paragraph = element("p");
    paragraphLines.forEach((text, position) => {
      if (position) paragraph.append(element("br"));
      paragraph.append(...inlineNodes(text));
    });
    fragment.append(paragraph);
  }
  return fragment;
}

/** conversationMarkdown is the export form of one conversation. */
export function conversationMarkdown(turns) {
  const lines = ["# 智能体对话记录", ""];
  for (const turn of turns) {
    lines.push(turn.role === "user" ? "## 我的提问" : "## 智能体回答", "", turn.text, "");
  }
  return lines.join("\n");
}
