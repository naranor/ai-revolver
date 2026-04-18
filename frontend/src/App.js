import React, { useState, useEffect } from 'react';
import { BrowserRouter as Router, Routes, Route, NavLink } from 'react-router-dom';
import Config from './pages/Config';
import Stats from './pages/Stats';
import Test from './pages/Test';

function App() {
  const [theme, setTheme] = useState(localStorage.getItem('theme') || 'dark');

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
  }, [theme]);

  const toggleTheme = () => {
    setTheme((prev) => (prev === 'light' ? 'dark' : 'light'));
  };

  return (
    <Router>
      <div className="app-container">
        <aside className="sidebar">
          <div style={{ padding: '32px 24px', borderBottom: '1px solid var(--border-strong)' }}>
            <div
              style={{
                fontSize: '14px',
                fontWeight: '800',
                letterSpacing: '0.1em',
                textTransform: 'uppercase',
              }}
            >
              AI Revolver <span style={{ color: 'var(--accent)' }}>v2.0</span>
            </div>
            <div
              style={{
                fontSize: '10px',
                color: 'var(--text-muted)',
                marginTop: '4px',
                fontFamily: 'var(--font-mono)',
              }}
            >
              Tactical Gateway
            </div>
          </div>

          <nav style={{ marginTop: '16px' }}>
            <NavLink
              to="/"
              className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}
              end
            >
              📊 Performance
            </NavLink>
            <NavLink
              to="/config"
              className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}
            >
              ⚙️ Loadout
            </NavLink>
            <NavLink
              to="/test"
              className={({ isActive }) => (isActive ? 'nav-item active' : 'nav-item')}
            >
              🧪 Ballistics
            </NavLink>
          </nav>

          <div className="theme-switch">
            <button
              className="btn"
              style={{ width: '100%', justifyContent: 'center' }}
              onClick={toggleTheme}
            >
              {theme === 'light' ? '🌙 Night Ops' : '☀️ Day Ops'}
            </button>
          </div>
        </aside>

        <main className="main-content">
          <Routes>
            <Route path="/" element={<Stats />} />
            <Route path="/config" element={<Config />} />
            <Route path="/test" element={<Test />} />
          </Routes>
        </main>
      </div>
    </Router>
  );
}

export default App;
