type Node = { type: string; value?: string; children?: Node[]; url?: string };

/** Parse the package's numbered-evidence-markdown.v1 syntax in prose only. */
export function remarkEvidenceCitations(evidence: readonly { id: string; citationId?: number }[]) {
  const citations = new Map(evidence.filter((item) => item.citationId !== undefined)
    .map((item) => [item.citationId!, item.id]));
  return () => (tree: Node) => {
    const visit = (node: Node) => {
      if (!node.children || node.type === "link" || node.type === "linkReference") return;
      node.children = node.children.flatMap((child) => {
        if (child.type !== "text" || !child.value) { visit(child); return [child]; }
        const parts: Node[] = [];
        let offset = 0;
        for (const match of child.value.matchAll(/\[(\d+)\]/g)) {
          const id = citations.get(Number(match[1]));
          if (!id) continue;
          if (match.index > offset) parts.push({ type: "text", value: child.value.slice(offset, match.index) });
          parts.push({ type: "link", url: `#evidence-${encodeURIComponent(id)}`,
            children: [{ type: "text", value: match[1] }] });
          offset = match.index + match[0].length;
        }
        if (offset < child.value.length) parts.push({ type: "text", value: child.value.slice(offset) });
        return parts;
      });
    };
    visit(tree);
  };
}
