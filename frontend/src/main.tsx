import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
// MiSans 在设计体系内按实际字重加载，字体与许可来自本地 RhineLabUI。
import "./index.css";
import './styles/workspace.css';
import './styles/motion.css';

// The composer handles selected files. Drops elsewhere must not navigate the WebView to a file.
const preventFileNavigation = (event: DragEvent) => {
  if (event.dataTransfer?.types.includes('Files')) event.preventDefault();
};
window.addEventListener('dragover', preventFileNavigation);
window.addEventListener('drop', preventFileNavigation);
if (import.meta.hot) import.meta.hot.dispose(() => {
  window.removeEventListener('dragover', preventFileNavigation);
  window.removeEventListener('drop', preventFileNavigation);
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
