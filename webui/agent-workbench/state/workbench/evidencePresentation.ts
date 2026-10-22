import type { ApiEvidence } from "./remoteModelAdapter";

export type EvidenceDocument = { key: string; title: string; items: ApiEvidence[] };

/** Preserve the source title without adding a file extension. */
export function evidenceDocumentTitle(item: ApiEvidence): string {
  return item.title;
}

/** Only a verified document identity may combine distinct retrieval passages. */
export function groupEvidenceDocuments(items: ApiEvidence[]): EvidenceDocument[] {
  const documents = new Map<string, EvidenceDocument>();
  for (const item of items) {
    const key = item.source ? `document:${item.source.documentId}` : `evidence:${item.id}`;
    const group = documents.get(key);
    if (group) group.items.push(item);
    else documents.set(key, { key, title: evidenceDocumentTitle(item), items: [item] });
  }
  return [...documents.values()];
}

/** Preserve source-provided line boundaries; do not infer or summarize sentence spans. */
export function sourceParagraphs(quote: string): string[] {
  return quote.split(/\r\n|\n|\r/).filter((line) => line.trim().length > 0);
}
