import React from "react";
import ReactDOM from "react-dom/client";

import { App } from "./App";
import "./styles.css";

// 挂载整个 React 应用，并让开发期严格模式帮助发现副作用问题。
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
