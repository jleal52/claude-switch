import React from 'react';
import ReactDOM from 'react-dom/client';
import './styles/globals.css';

function App() {
  return <h1 className="p-4 text-2xl font-bold">claude-switch</h1>;
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
