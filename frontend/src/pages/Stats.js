import React, { useState, useEffect, useMemo } from 'react';
import axios from 'axios';

function Sparkline({ data, color = 'var(--success)', height = 30 }) {
  if (!data || data.length < 2) return null;

  const max = Math.max(...data);
  const min = Math.min(...data);
  const range = max - min || 1;

  const points = data
    .map((val, i) => {
      const x = (i / (data.length - 1)) * 100;
      const y = height - ((val - min) / range) * (height - 4) - 2;
      return `${x},${y}`;
    })
    .join(' ');

  return (
    <svg width="80" height={height} style={{ overflow: 'visible' }}>
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function RadialGauge({ value, max = 100, size = 50, color = 'var(--success)' }) {
  const radius = (size - 6) / 2;
  const circumference = 2 * Math.PI * radius;
  const percent = Math.min(value / max, 1);
  const offset = circumference * (1 - percent);

  return (
    <svg width={size} height={size}>
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="var(--border)"
        strokeWidth="3"
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke={color}
        strokeWidth="3"
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        style={{ transition: 'stroke-dashoffset 0.5s ease' }}
      />
    </svg>
  );
}

function TrendNumber({ value, previousValue }) {
  const diff = value - previousValue;
  const percentChange = previousValue > 0 ? ((diff / previousValue) * 100).toFixed(1) : 0;
  const isPositive = diff >= 0;
  const trendColor = isPositive ? 'var(--success)' : 'var(--danger)';

  if (previousValue === undefined || previousValue === 0) {
    return (
      <span className="mono" style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
        --
      </span>
    );
  }

  return (
    <span className="mono" style={{ fontSize: '12px', color: trendColor }}>
      {isPositive ? '↑' : '↓'} {Math.abs(percentChange)}%
    </span>
  );
}

function StatCard({
  label,
  value,
  history,
  accentColor,
  showGauge,
  gaugeValue,
  currentValue,
  showSparkline,
  unit,
}) {
  const historyData = useMemo(() => {
    if (!history || history.length === 0) return [];
    return history.map((h) => h[value] || 0);
  }, [history, value]);

  const previousValue =
    showSparkline && historyData.length > 1 ? historyData[historyData.length - 2] : undefined;
  const displayValue =
    currentValue !== undefined
      ? currentValue
      : historyData.length > 0
        ? historyData[historyData.length - 1]
        : 0;

  return (
    <div
      className="panel"
      style={{ marginBottom: 0, borderLeft: `3px solid ${accentColor || 'var(--border)'}` }}
    >
      <div className="panel-body" style={{ padding: '16px' }}>
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'flex-start',
            marginBottom: '12px',
          }}
        >
          <div
            className="field-label"
            style={{ fontSize: '10px', textTransform: 'uppercase', letterSpacing: '0.5px' }}
          >
            {label}
          </div>
          {showGauge && <RadialGauge value={gaugeValue} size={40} color={accentColor} />}
        </div>

        <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between' }}>
          <div>
            <div
              className="mono"
              style={{ fontSize: '28px', fontWeight: '700', lineHeight: 1, marginBottom: '4px' }}
            >
              {typeof displayValue === 'number' ? displayValue.toLocaleString() : '--'}
              {unit && (
                <span
                  style={{ fontSize: '14px', fontWeight: 400, marginLeft: '4px', opacity: 0.7 }}
                >
                  {unit}
                </span>
              )}
            </div>
            {showSparkline && <TrendNumber value={displayValue} previousValue={previousValue} />}
          </div>

          {showSparkline && historyData.length > 1 && (
            <Sparkline data={historyData.slice(-20)} color={accentColor} height={28} />
          )}
        </div>
      </div>
    </div>
  );
}

function Stats() {
  const [stats, setStats] = useState(null);
  const [history, setHistory] = useState([]);
  const [activeModel, setActiveModel] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [sortConfig, setSortConfig] = useState({ key: 'total', direction: 'desc' });
  const [period, setPeriod] = useState('hour');

  useEffect(() => {
    fetchData();
    const interval = setInterval(fetchData, 5000);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [period]);

  const fetchData = async () => {
    try {
      const [statsRes, historyRes, activeModelRes] = await Promise.all([
        axios.get(`/api/v1/stats${period ? `?period=${period}` : ''}`),
        axios.get(`/api/v1/stats/history${period ? `?period=${period}` : ''}`),
        axios.get('/api/v1/active-model'),
      ]);
      setStats(statsRes.data);
      setHistory(historyRes.data);
      setActiveModel(activeModelRes.data);
      setLoading(false);
    } catch (err) {
      setError(err.message);
      setLoading(false);
    }
  };

  const requestSort = (key) => {
    let direction = 'asc';
    if (sortConfig.key === key && sortConfig.direction === 'asc') {
      direction = 'desc';
    }
    setSortConfig({ key, direction });
  };

  const sortedModels = React.useMemo(() => {
    if (!stats || !stats.models) return [];
    return [...stats.models].sort((a, b) => {
      const aVal = a[sortConfig.key] ?? 0;
      const bVal = b[sortConfig.key] ?? 0;
      if (aVal < bVal) {
        return sortConfig.direction === 'asc' ? -1 : 1;
      }
      if (aVal > bVal) {
        return sortConfig.direction === 'asc' ? 1 : -1;
      }
      return 0;
    });
  }, [stats, sortConfig]);

  const efficiency = stats
    ? Math.round((stats.successful_requests / stats.total_requests) * 100) || 0
    : 0;
  const avgLatency = stats && stats.total_requests > 0 ? Math.round(stats.avg_latency || 0) : null;

  if (loading)
    return (
      <div className="panel">
        <div className="panel-body mono">READING TELEMETRY...</div>
      </div>
    );
  if (error)
    return (
      <div className="panel">
        <div className="panel-body mono status-danger">LINK ERROR: {error}</div>
      </div>
    );

  return (
    <div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'flex-end',
          marginBottom: '32px',
        }}
      >
        <div>
          <h1>Global Telemetry</h1>
          <div
            style={{
              color: 'var(--success)',
              fontSize: '12px',
              fontFamily: 'var(--font-mono)',
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
            }}
          >
            <span
              className="status-indicator status-success"
              style={{ animation: 'pulse 2s infinite' }}
            ></span>
            LIVE FEED ACTIVE
          </div>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          {['', 'hour', 'day', 'week', 'month'].map((p) => (
            <button
              key={p || 'all'}
              onClick={() => setPeriod(p)}
              className={`btn ${period === p ? 'btn-primary' : 'btn-ghost'}`}
              style={{ fontSize: '11px', padding: '6px 12px' }}
            >
              {p ? p.toUpperCase() : 'ALL'}
            </button>
          ))}
        </div>
      </div>

      <div
        className="panel"
        style={{
          marginBottom: '24px',
          borderLeft: `3px solid ${activeModel && activeModel.active ? 'var(--accent)' : 'var(--border)'}`,
        }}
      >
        <div
          className="panel-body"
          style={{ padding: '16px', display: 'flex', alignItems: 'center', gap: '16px' }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span
              className={`status-indicator ${activeModel && activeModel.active ? 'status-success' : 'status-muted'}`}
              style={{
                animation: activeModel && activeModel.active ? 'pulse 2s infinite' : 'none',
              }}
            ></span>
            <span
              className="field-label"
              style={{ fontSize: '10px', textTransform: 'uppercase', letterSpacing: '0.5px' }}
            >
              ACTIVE AUTO MODEL
            </span>
          </div>
          <div style={{ flex: 1 }}>
            {activeModel && activeModel.active ? (
              <div className="mono" style={{ fontSize: '14px', fontWeight: '600' }}>
                <span style={{ color: 'var(--text-muted)' }}>{activeModel.provider}</span>
                <span style={{ margin: '0 8px', color: 'var(--border-strong)' }}>/</span>
                <span style={{ color: 'var(--accent)' }}>{activeModel.model}</span>
                <span
                  style={{
                    marginLeft: '16px',
                    fontSize: '12px',
                    color:
                      activeModel.latency > 1000
                        ? 'var(--danger)'
                        : activeModel.latency > 500
                          ? 'var(--warning)'
                          : 'var(--success)',
                  }}
                >
                  {activeModel.latency}ms
                </span>
              </div>
            ) : (
              <div className="mono" style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                Waiting for first successful request...
              </div>
            )}
          </div>
          <div
            style={{ fontSize: '11px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}
          >
            {activeModel && activeModel.active ? 'PRIORITY: NEXT REQUEST' : 'STATUS: IDLE'}
          </div>
        </div>
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
          gap: '16px',
          marginBottom: '40px',
        }}
      >
        <StatCard
          label="Total Engagements"
          value="total_requests"
          history={history}
          accentColor="var(--accent)"
          currentValue={stats.total_requests}
          showSparkline={true}
        />
        <StatCard
          label="Successful Hits"
          value="successful_requests"
          history={history}
          accentColor="var(--success)"
          currentValue={stats.successful_requests}
          showSparkline={true}
        />
        <StatCard
          label="System Failures"
          value="failed_requests"
          history={history}
          accentColor="var(--danger)"
          currentValue={stats.failed_requests}
          showSparkline={true}
        />
        <StatCard
          label="Rate Inhibitions"
          value="rate_limit_events"
          history={history}
          accentColor="var(--warning)"
          currentValue={stats.rate_limit_events}
          showSparkline={true}
        />
        <StatCard
          label="Avg Latency"
          value="avg_latency"
          history={history}
          accentColor="var(--text-muted)"
          currentValue={avgLatency}
          showSparkline={true}
          unit="ms"
        />
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '16px', marginBottom: '24px' }}>
        <h2>Ballistic Performance</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginLeft: 'auto' }}>
          <span className="field-label" style={{ fontSize: '10px' }}>
            SYSTEM EFFICIENCY
          </span>
          <RadialGauge
            value={efficiency}
            size={36}
            color={
              efficiency > 90
                ? 'var(--success)'
                : efficiency > 70
                  ? 'var(--warning)'
                  : 'var(--danger)'
            }
          />
          <span className="mono" style={{ fontSize: '16px', fontWeight: '700' }}>
            {efficiency}%
          </span>
        </div>
      </div>

      <div className="panel">
        <table className="data-table">
          <thead>
            <tr>
              <th style={{ width: '40px' }}></th>
              <th onClick={() => requestSort('id')}>Trajectory ID</th>
              <th onClick={() => requestSort('total')}>
                Count{sortConfig.key === 'total' && (sortConfig.direction === 'asc' ? ' ↑' : ' ↓')}
              </th>
              <th onClick={() => requestSort('successful')}>Efficiency</th>
              <th onClick={() => requestSort('latency')}>Avg Latency</th>
            </tr>
          </thead>
          <tbody>
            {sortedModels.map((m, i) => {
              const modelEff = m.total > 0 ? Math.round((m.successful / m.total) * 100) : 0;
              const isBlocked = m.is_blocked === true;
              return (
                <tr key={i} style={{ opacity: isBlocked ? 0.5 : 1 }}>
                  <td style={{ textAlign: 'center' }}>
                    <span
                      className={`status-indicator ${isBlocked ? 'status-danger' : 'status-success'}`}
                      style={{
                        animation: isBlocked ? 'none' : 'pulse 2s infinite',
                        display: 'inline-block',
                      }}
                    ></span>
                  </td>
                  <td className="mono" style={{ fontWeight: '600' }}>
                    <span style={{ color: isBlocked ? 'var(--text-muted)' : 'var(--text)' }}>
                      {m.id}
                    </span>
                    {isBlocked && (
                      <span
                        style={{
                          marginLeft: '8px',
                          fontSize: '9px',
                          padding: '2px 6px',
                          backgroundColor: 'rgba(255, 69, 58, 0.2)',
                          color: 'var(--danger)',
                          borderRadius: '2px',
                          textTransform: 'uppercase',
                          letterSpacing: '0.5px',
                        }}
                      >
                        BLOCKED {m.last_code > 0 ? m.last_code : ''}
                      </span>
                    )}
                  </td>
                  <td
                    className="mono"
                    style={{ color: isBlocked ? 'var(--text-muted)' : 'inherit' }}
                  >
                    {m.total}
                  </td>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                      <div
                        style={{
                          flex: 1,
                          height: '6px',
                          backgroundColor: 'var(--border)',
                          width: '80px',
                        }}
                      >
                        <div
                          style={{
                            height: '100%',
                            backgroundColor:
                              modelEff > 90
                                ? 'var(--success)'
                                : modelEff > 70
                                  ? 'var(--warning)'
                                  : modelEff > 30
                                    ? 'var(--danger)'
                                    : 'var(--text-muted)',
                            width: `${modelEff}%`,
                            opacity: isBlocked ? 0.4 : 1,
                          }}
                        ></div>
                      </div>
                      <span
                        className="mono"
                        style={{
                          fontSize: '11px',
                          minWidth: '36px',
                          textAlign: 'right',
                          color: isBlocked ? 'var(--text-muted)' : 'inherit',
                        }}
                      >
                        {modelEff}%
                      </span>
                    </div>
                  </td>
                  <td
                    className="mono"
                    style={{ color: isBlocked ? 'var(--text-muted)' : 'inherit' }}
                  >
                    {m.latency}ms
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default Stats;
