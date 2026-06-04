import React from "react";
import ReactDOM from "react-dom/client";

import { App } from "./App";
import { OnboardingSettingsDemo } from "./components/onboarding/OnboardingSettingsDemo";
import "./styles.css";

// 根据查询参数切换到独立 demo 页面，避免影响正式应用入口。
function RootApp() {
  const searchParams = new URLSearchParams(window.location.search);
  if (searchParams.get("demo") === "onboarding-settings") {
    return <OnboardingSettingsDemo />;
  }
  return <App />;
}

// 挂载整个 React 应用，并让开发期严格模式帮助发现副作用问题。
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <RootApp />
  </React.StrictMode>
);
