/**
 * feature_id: v1.workbench.chat
 * route_id: workbench.chat
 * component_id: workbench.chat.pdf_reader
 * state_owner: workbench.chat.selected_evidence
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, LoaderCircle, Minus, Plus, X } from "lucide-react";
import { getDocument, GlobalWorkerOptions, type PDFDocumentProxy, type RenderTask } from "pdfjs-dist";
import pdfWorkerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import type { ApiTurnResult } from "../../../state/workbench/remoteModelAdapter";

GlobalWorkerOptions.workerSrc = pdfWorkerUrl;

type Highlight = { left: number; top: number; width: number; height: number };

export function PdfReader({ result, evidenceId, onClose }: {
  result: ApiTurnResult;
  evidenceId: string;
  onClose: () => void;
}) {
  const initial = result.evidence.find((item) => item.id === evidenceId);
  const references = useMemo(() => result.evidence.filter((item) => item.source
    && item.source.documentId === initial?.source?.documentId), [result, initial]);
  const [referenceIndex, setReferenceIndex] = useState(0);
  const reference = references[referenceIndex];
  const source = reference?.source;
  const [pdf, setPdf] = useState<PDFDocumentProxy | null>(null);
  const [pageNumber, setPageNumber] = useState(1);
  const [zoom, setZoom] = useState(1);
  const [width, setWidth] = useState(0);
  const [dimensions, setDimensions] = useState({ width: 1, height: 1 });
  const [highlights, setHighlights] = useState<Highlight[]>([]);
  const [error, setError] = useState("");
  const [rendering, setRendering] = useState(true);
  const [renderedPage, setRenderedPage] = useState<{ evidenceId: string; page: number; width: number; zoom: number } | null>(null);
  const pageReady = renderedPage?.evidenceId === reference?.id && renderedPage?.page === pageNumber
    && renderedPage?.width === width && renderedPage?.zoom === zoom;
  const viewportRef = useRef<HTMLDivElement>(null);
  const pageRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const prior = document.activeElement;
    closeRef.current?.focus();
    return () => { if (prior instanceof HTMLElement && prior.isConnected) prior.focus(); };
  }, []);

  useEffect(() => {
    const index = references.findIndex((item) => item.id === evidenceId);
    setReferenceIndex(Math.max(0, index));
  }, [evidenceId, references]);

  useEffect(() => {
    setPageNumber(source?.locations[0].page ?? 1);
  }, [source]);

  useEffect(() => {
    const element = viewportRef.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    setPdf(null);
    setError("");
    setRendering(true);
    setRenderedPage(null);
    if (!source) { setError("该引用未提供 PDF 定位信息。"); return; }
    let disposed = false;
    const task = getDocument({ url: source.url, withCredentials: true, isEvalSupported: false });
    void task.promise.then((document) => {
      if (!disposed) setPdf(document);
    }).catch((reason: unknown) => {
      if (!disposed) setError(reason instanceof Error ? reason.message : "PDF 读取失败。");
    });
    return () => { disposed = true; void task.destroy(); };
  }, [source?.url]);

  useEffect(() => {
    if (!pdf || !source || !width) return;
    let disposed = false;
    let task: RenderTask | undefined;
    setRendering(true);
    setError("");
    setHighlights([]);
    void (async () => {
      const page = await pdf.getPage(pageNumber);
      if (disposed || !canvasRef.current) return;
      const base = page.getViewport({ scale: 1 });
      const scale = Math.max(1, width - 32) / base.width * zoom;
      const viewport = page.getViewport({ scale });
      const canvas = canvasRef.current;
      const context = canvas.getContext("2d");
      if (!context) throw new Error("浏览器无法显示 PDF 页面。");
      const ratio = window.devicePixelRatio || 1;
      canvas.width = Math.ceil(viewport.width * ratio);
      canvas.height = Math.ceil(viewport.height * ratio);
      setDimensions({ width: viewport.width, height: viewport.height });
      const [originX, originY, endX, endY] = page.view;
      const locations = source.locations.filter((location) => location.page === pageNumber);
      const boxes = locations.map(({ rect: [left, top, right, bottom] }) => {
        if (left < 0 || top < 0 || right > endX - originX || bottom > endY - originY) {
          throw new Error("引用坐标超出 PDF 页面，无法准确定位。");
        }
        // Source coordinates are top-left page points; PDF.js handles page rotation.
        const [x1, y1, x2, y2] = viewport.convertToViewportRectangle([
          originX + left, endY - top, originX + right, endY - bottom,
        ]);
        return { left: Math.min(x1, x2), top: Math.min(y1, y2), width: Math.abs(x2 - x1), height: Math.abs(y2 - y1) };
      });
      task = page.render({ canvas, canvasContext: context, viewport,
        transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0] });
      await task.promise;
      if (disposed) return;
      setHighlights(boxes);
      setRenderedPage({ evidenceId: reference!.id, page: pageNumber, width, zoom });
      setRendering(false);
      requestAnimationFrame(() => {
        if (disposed || !viewportRef.current || !pageRef.current) return;
        viewportRef.current.scrollTo({ top: pageRef.current.offsetTop + (boxes[0]?.top ?? 0) - 48 });
      });
    })().catch((reason: unknown) => {
      if (!disposed) {
        setError(reason instanceof Error ? reason.message : "PDF 页面显示失败。");
        setRendering(false);
      }
    });
    return () => { disposed = true; task?.cancel(); };
  }, [pdf, source, pageNumber, width, zoom]);

  return <aside className="pdf-reader" aria-label="PDF 阅读面板" data-component-id="workbench.chat.pdf_reader"
    data-run-id={result.runId} data-evidence-id={reference?.id} data-document-id={source?.documentId}>
    <header className="pdf-reader-header">
      <strong>{source?.fileName || initial?.source?.fileName}</strong>
      <button className="icon-button" type="button" ref={closeRef} onClick={onClose} title="关闭 PDF" aria-label="关闭 PDF"><X aria-hidden="true" /></button>
    </header>
    <div className="pdf-reader-toolbar">
      <div className="pdf-reference-switch" aria-label="引用段落">
        <button className="icon-button" type="button" title="上一处引用" disabled={referenceIndex <= 0}
          onClick={() => setReferenceIndex((index) => index - 1)}><ChevronLeft aria-hidden="true" /></button>
        <span>引用 {referenceIndex + 1} / {references.length}</span>
        <button className="icon-button" type="button" title="下一处引用" disabled={referenceIndex >= references.length - 1}
          onClick={() => setReferenceIndex((index) => index + 1)}><ChevronRight aria-hidden="true" /></button>
      </div>
      <div className="pdf-zoom-controls">
        <button className="icon-button" type="button" title="缩小 PDF" disabled={zoom <= 0.75} onClick={() => setZoom((value) => value - 0.25)}><Minus aria-hidden="true" /></button>
        <output>{Math.round(zoom * 100)}%</output>
        <button className="icon-button" type="button" title="放大 PDF" disabled={zoom >= 2} onClick={() => setZoom((value) => value + 0.25)}><Plus aria-hidden="true" /></button>
      </div>
    </div>
    <div className="pdf-page-navigation">
      <button className="icon-button" type="button" title="上一页" disabled={!pdf || pageNumber <= 1} onClick={() => setPageNumber((page) => page - 1)}><ChevronLeft aria-hidden="true" /></button>
      <label><span className="visually-hidden">PDF 页码</span><input aria-label="PDF 页码" type="number" min={1} max={pdf?.numPages ?? 1} value={pageNumber}
        onChange={(event) => { const value = event.target.valueAsNumber; if (pdf && Number.isInteger(value) && value >= 1 && value <= pdf.numPages) setPageNumber(value); }} /></label>
      <span>/ {pdf?.numPages ?? "..."}</span>
      <button className="icon-button" type="button" title="下一页" disabled={!pdf || pageNumber >= pdf.numPages} onClick={() => setPageNumber((page) => page + 1)}><ChevronRight aria-hidden="true" /></button>
    </div>
    <div className="pdf-viewport" ref={viewportRef}>
      {error && <p className="pdf-error" role="alert">{error}</p>}
      {(rendering || !pageReady) && !error && <div className="pdf-loading" role="status"><LoaderCircle className="spin" aria-hidden="true" /><span>正在读取 PDF</span></div>}
      <div className="pdf-page" ref={pageRef} data-rendered-evidence-id={pageReady ? renderedPage?.evidenceId : undefined}
        data-rendered-page={pageReady ? renderedPage?.page : undefined}
        style={{ width: dimensions.width, height: dimensions.height }} hidden={Boolean(error) || !pdf}>
        <canvas ref={canvasRef} aria-label={`PDF 第 ${pageNumber} 页`} style={{ width: dimensions.width, height: dimensions.height }} />
        {pageReady && highlights.map((box, index) => <mark className="pdf-highlight" key={index} style={box} aria-label="引用原文" data-page={pageNumber} />)}
      </div>
    </div>
  </aside>;
}
