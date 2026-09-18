import { AppsSDKUIProvider } from "@openai/apps-sdk-ui/components/AppsSDKUIProvider";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import App from "./App.tsx";
import "./components/ui/apps-sdk.css";
import "./components/ui/tokens.css";
import "./components/ui/components.css";
import "./styles.css";
import "./components/ui/sub2api-style.css";

const root = document.getElementById("root");

if (!root) throw new Error("console_root_missing");

createRoot(root).render(
  <StrictMode>
    <AppsSDKUIProvider linkComponent="a">
      <App />
    </AppsSDKUIProvider>
  </StrictMode>
);
