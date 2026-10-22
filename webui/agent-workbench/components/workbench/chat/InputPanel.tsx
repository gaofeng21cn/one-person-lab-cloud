/**
 * feature_id: v1.workbench.chat
 * route_id: workbench.chat
 * component_id: workbench.chat.input_panel
 * state_owner: workbench.chat.composer_state
 */
import {
  AuiIf,
  ComposerPrimitive,
} from "@assistant-ui/react";
import { ArrowUp, Square } from "lucide-react";

export function InputPanel({ disabled, canCancel = false }: { disabled: boolean; canCancel?: boolean }) {
  return (
    <div
      className="input-panel-wrap"
      data-feature-id="v1.workbench.chat"
      data-route-id="workbench.chat"
      data-component-id="workbench.chat.input_panel"
    >
      <div className="input-dropzone">
        <ComposerPrimitive.Root className="composer-root">
          <ComposerPrimitive.Input
            className="composer-input"
            placeholder="发送消息..."
            rows={1}
            disabled={disabled}
            maxLength={4000}
            unstable_insertNewlineOnTouchEnter
          />

          <div className="composer-actions">
            <div className="composer-actions-right">
              <AuiIf condition={(state) => state.thread.isRunning}>
                {canCancel ? <ComposerPrimitive.Cancel className="send-button stop-button" title="停止生成" aria-label="停止生成">
                  <Square aria-hidden="true" />
                </ComposerPrimitive.Cancel> : <button className="send-button" type="button" title="正在回答，请稍候" aria-label="正在回答，请稍候" disabled>
                  <ArrowUp aria-hidden="true" />
                </button>}
              </AuiIf>
              <AuiIf condition={(state) => !state.thread.isRunning}>
                <ComposerPrimitive.Send className="send-button" title="发送" disabled={disabled}>
                  <ArrowUp aria-hidden="true" />
                </ComposerPrimitive.Send>
              </AuiIf>
            </div>
          </div>
        </ComposerPrimitive.Root>
      </div>
    </div>
  );
}
