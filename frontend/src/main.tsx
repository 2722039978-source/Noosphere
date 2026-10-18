import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
// MiSans 在设计体系内按实际字重加载，字体与许可来自本地 RhineLabUI。
import "./index.css";
import './styles/workspace.css';
import './styles/motion.css';

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
