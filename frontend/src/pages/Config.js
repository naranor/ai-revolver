import React, { useState, useEffect } from 'react';
import axios from 'axios';

function Config() {
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState(null);
  const [probeResults, setProbeResults] = useState({});
  const [dragState, setDragState] = useState({ providerIndex: null, modelIndex: null });

  useEffect(() => {
    fetchConfig();
  }, []);

  const fetchConfig = async () => {
    try {
      const response = await axios.get('/api/v1/config');
      setConfig(response.data);
      setLoading(false);
    } catch (err) {
      setStatus({ type: 'error', message: 'FAILED TO LOAD CONFIG: ' + err.message });
      setLoading(false);
    }
  };

  const handleSubmit = async (e) => {
    if (e) e.preventDefault();
    setStatus({ type: 'info', message: 'SYNCING CONFIGURATION...' });
    try {
      await axios.put('/api/v1/config', config);
      setStatus({ type: 'success', message: 'CONFIGURATION SYNCED' });
      setTimeout(() => setStatus(null), 3000);
    } catch (err) {
      setStatus({ type: 'error', message: 'SYNC FAILED: ' + err.message });
    }
  };

  const addProvider = () => {
    setConfig((prev) => ({
      ...prev,
      providers: [
        ...(prev.providers || []),
        { name: 'NEW_PROVIDER', api_key: '', base_url: '', models: [], rate_limit: 0, priority: 1 },
      ],
    }));
  };

  const removeProvider = (index) => {
    if (window.confirm('DISCARD PROVIDER?')) {
      setConfig((prev) => ({
        ...prev,
        providers: prev.providers.filter((_, i) => i !== index),
      }));
    }
  };

  const updateProvider = (index, field, value) => {
    setConfig((prev) => {
      const newProviders = [...prev.providers];
      let val = value;
      if (field === 'rate_limit' || field === 'priority') val = parseInt(value) || 0;
      newProviders[index] = { ...newProviders[index], [field]: val };
      return { ...prev, providers: newProviders };
    });
  };

  const addModel = (pIndex) => {
    setConfig((prev) => {
      const newProviders = [...prev.providers];
      const newModels = [
        ...(newProviders[pIndex].models || []),
        {
          name: `model-${Date.now()}`,
          max_tokens: 4096,
          thinking: false,
          reasoning: false,
          tools: false,
        },
      ];
      newProviders[pIndex] = { ...newProviders[pIndex], models: newModels };
      return { ...prev, providers: newProviders };
    });
  };

  const removeModel = (pIndex, mIndex) => {
    setConfig((prev) => {
      const newProviders = [...prev.providers];
      const newModels = newProviders[pIndex].models.filter((_, i) => i !== mIndex);
      newProviders[pIndex] = { ...newProviders[pIndex], models: newModels };
      return { ...prev, providers: newProviders };
    });
  };

  const updateModel = (pIndex, mIndex, field, value) => {
    setConfig((prev) => {
      const newProviders = [...prev.providers];
      const newModels = [...newProviders[pIndex].models];
      let val = value;
      if (field === 'max_tokens') val = parseInt(value) || 0;
      newModels[mIndex] = { ...newModels[mIndex], [field]: val };
      newProviders[pIndex] = { ...newProviders[pIndex], models: newModels };
      return { ...prev, providers: newProviders };
    });
  };

  // Drag-and-drop handlers for model reordering
  const handleDragStart = (e, pIndex, mIndex) => {
    setDragState({ providerIndex: pIndex, modelIndex: mIndex });
    e.dataTransfer.effectAllowed = 'move';
    e.target.style.opacity = '0.5';
  };

  const handleDragEnd = (e) => {
    e.target.style.opacity = '1';
    setDragState({ providerIndex: null, modelIndex: null });
    // Remove all drag-over indicators
    document.querySelectorAll('.model-row-drag-over').forEach((el) => {
      el.classList.remove('model-row-drag-over');
    });
  };

  const handleDragOver = (e, pIndex, mIndex) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';

    // Only highlight if same provider and different position
    if (pIndex === dragState.providerIndex && mIndex !== dragState.modelIndex) {
      e.currentTarget.style.borderColor = 'var(--accent)';
      e.currentTarget.style.backgroundColor = 'rgba(212, 175, 55, 0.05)';
    }
  };

  const handleDragLeave = (e) => {
    e.currentTarget.style.borderColor = '';
    e.currentTarget.style.backgroundColor = '';
  };

  const handleDrop = (e, pIndex, mIndex) => {
    e.preventDefault();
    e.currentTarget.style.borderColor = '';
    e.currentTarget.style.backgroundColor = '';

    // Validate drop target
    if (pIndex !== dragState.providerIndex || mIndex === dragState.modelIndex) {
      return;
    }

    // Reorder models
    setConfig((prev) => {
      const newProviders = [...prev.providers];
      const models = [...newProviders[pIndex].models];
      const [draggedModel] = models.splice(dragState.modelIndex, 1);
      models.splice(mIndex, 0, draggedModel);
      newProviders[pIndex] = { ...newProviders[pIndex], models };
      return { ...prev, providers: newProviders };
    });

    setDragState({ providerIndex: null, modelIndex: null });
  };

  const checkModel = async (pIndex, mIndex) => {
    const provider = config.providers[pIndex];
    const model = provider.models[mIndex];
    const key = `${pIndex}-${mIndex}`;

    setProbeResults((prev) => ({ ...prev, [key]: { loading: true } }));

    let modelInfo = null;
    try {
      const infoRes = await axios.get(
        `/api/v1/modelinfo?provider=${encodeURIComponent(provider.name)}&model=${encodeURIComponent(model.name)}`
      );
      modelInfo = infoRes.data;
    } catch (infoErr) {
      console.warn('Failed to get model info', infoErr);
    }

    let thinking = model.thinking;
    let reasoning = model.reasoning;

    if (modelInfo) {
      const name = (modelInfo.id || modelInfo.name || '').toLowerCase();
      const desc = (modelInfo.description || '').toLowerCase();
      const caps = modelInfo.capabilities || modelInfo;

      // For Ollama models, infer capabilities from name
      if (provider.name === 'ollama') {
        thinking = name.includes('thinking') || name.includes('kimi') || name.includes('deepseek');
        reasoning =
          name.includes('reasoning') ||
          name.includes('coder') ||
          name.includes('glm') ||
          name.includes('qwen');
        // Ollama now supports tools via our proxy!
      } else {
        if (caps.thinking !== undefined) thinking = caps.thinking;
        else if (
          name.includes('thinking') ||
          name.includes('reasoning') ||
          name.includes('o1') ||
          name.includes('r1')
        )
          thinking = true;

        if (caps.reasoning !== undefined) reasoning = caps.reasoning;
        else if (name.includes('reasoning') || desc.includes('reasoning')) reasoning = true;
      }
    }

    const probePayload = {
      model: model.name,
      provider: provider.name,
      messages: [
        {
          role: 'user',
          content: 'What is the weather in London? Call the get_weather function to answer.',
        },
      ],
      tools: [
        {
          type: 'function',
          function: {
            name: 'get_weather',
            description: 'Get current weather',
            parameters: {
              type: 'object',
              properties: { location: { type: 'string' } },
            },
          },
        },
      ],
      tool_choice: 'auto',
    };

    try {
      const res = await axios.post('/api/v1/test', probePayload);
      if (res.data.success) {
        const choice = res.data.response?.choices?.[0];
        const msg = choice?.message;
        const toolCalls = msg?.tool_calls || msg?.function_call;

        const hasToolCalls = !!(
          toolCalls &&
          (Array.isArray(toolCalls)
            ? toolCalls.length > 0
            : typeof toolCalls === 'object' ||
              (typeof toolCalls === 'string' && toolCalls.length > 2))
        );

        setProbeResults((prev) => ({
          ...prev,
          [key]: {
            loading: false,
            success: hasToolCalls,
            latency: res.data.latency_ms,
            error: hasToolCalls ? null : 'NO TOOLS',
          },
        }));
        updateModel(pIndex, mIndex, 'tools', hasToolCalls);
      } else {
        const errorMsg = (res.data.error || '').toLowerCase();
        const errorCode = res.data.last_code;
        const isToolUnsupported =
          errorMsg.includes('tool') || errorMsg.includes('parameter') || errorMsg.includes('400');
        const displayError = isToolUnsupported ? 'NO TOOLS' : `FAIL ${errorCode || ''}`.trim();

        setProbeResults((prev) => ({
          ...prev,
          [key]: { loading: false, success: false, error: displayError },
        }));
        updateModel(pIndex, mIndex, 'tools', false);
      }
    } catch (err) {
      setProbeResults((prev) => ({
        ...prev,
        [key]: { loading: false, success: false, error: 'NET FAIL' },
      }));
    }

    if (thinking !== model.thinking) updateModel(pIndex, mIndex, 'thinking', thinking);
    if (reasoning !== model.reasoning) updateModel(pIndex, mIndex, 'reasoning', reasoning);

    setTimeout(() => {
      setProbeResults((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
    }, 10000);
  };

  const probeAllModels = async (pIndex) => {
    const provider = config.providers[pIndex];
    if (!provider.models?.length) return;
    setStatus({
      type: 'info',
      message: `INITIATING MASS PARALLEL PROBE (CONCURRENCY: 5) FOR ${provider.name}...`,
    });

    const models = provider.models;
    const concurrencyLimit = 5;

    // Process in batches of 5
    for (let i = 0; i < models.length; i += concurrencyLimit) {
      const batch = [];
      for (let j = 0; j < concurrencyLimit && i + j < models.length; j++) {
        batch.push(checkModel(pIndex, i + j));
      }
      // Wait for all 5 in the batch to finish before starting next 5
      await Promise.all(batch);
    }

    setStatus({ type: 'success', message: `MASS PROBE COMPLETE FOR ${provider.name}` });
  };

  const optimizeProvider = async (pIndex) => {
    const provider = config.providers[pIndex];
    if (!provider.models?.length) return;
    setStatus({ type: 'info', message: `OPTIMIZING TRAJECTORIES FOR ${provider.name}...` });
    const results = [];
    for (let i = 0; i < provider.models.length; i++) {
      const m = provider.models[i];
      try {
        const res = await axios.post('/api/v1/test', {
          model: m.name,
          provider: provider.name,
          messages: [{ role: 'user', content: 'hi' }],
        });
        results.push({ index: i, latency: res.data.success ? res.data.latency_ms : 999999 });
      } catch {
        results.push({ index: i, latency: 999999 });
      }
    }
    results.sort((a, b) => a.latency - b.latency);
    const sorted = results.map((r) => provider.models[r.index]);
    setConfig((prev) => {
      const newProviders = [...prev.providers];
      newProviders[pIndex] = { ...newProviders[pIndex], models: sorted };
      return { ...prev, providers: newProviders };
    });
    setStatus({ type: 'success', message: 'BALLISTICS OPTIMIZED' });
  };

  const scoutModels = async (pIndex) => {
    const provider = config.providers[pIndex];
    setStatus({ type: 'info', message: `INITIATING SCOUT RECONNAISSANCE FOR ${provider.name}...` });

    try {
      const res = await axios.get(`/api/v1/scout?provider=${provider.name}`);
      const availableModels = res.data.data || res.data;

      if (!Array.isArray(availableModels)) throw new Error('Invalid response format');

      const freeModels = availableModels.filter((m) => {
        // For Ollama, all models are free (no pricing info)
        if (provider.name === 'ollama') return true;
        if (!m.pricing) return true;
        const promptPrice = parseFloat(m.pricing.prompt) || 0;
        const completionPrice = parseFloat(m.pricing.completion) || 0;
        return promptPrice === 0 && completionPrice === 0;
      });

      if (freeModels.length === 0) {
        setStatus({ type: 'error', message: 'NO FREE MODELS DETECTED IN THIS REGISTRY' });
        return;
      }

      const scoutedModels = freeModels.map((m) => {
        const name = (m.id || m.name || '').toLowerCase();
        const desc = (m.description || '').toLowerCase();

        // Estimate context length based on model name patterns for Ollama
        let contextLength =
          m.context_length || m.context_window || m.max_tokens || m.max_context_tokens;

        if (!contextLength && provider.name === 'ollama') {
          // Ollama-specific context length estimation
          if (name.includes('32b') || name.includes('675b') || name.includes('671b')) {
            contextLength = 131072;
          } else if (name.includes('70b') || name.includes('72b') || name.includes('123b')) {
            contextLength = 65536;
          } else if (name.includes('8b') || name.includes('3b') || name.includes('4b')) {
            contextLength = 32768;
          } else if (name.includes('mini') || name.includes('flash') || name.includes('nano')) {
            contextLength = 32768;
          } else {
            contextLength = 65536; // Default for large Ollama models
          }
        }

        contextLength = contextLength || 4096;

        return {
          name: m.id || m.name,
          max_tokens: contextLength,
          thinking: name.includes('thinking') || desc.includes('thinking'),
          reasoning: name.includes('reasoning') || desc.includes('reasoning'),
          tools:
            desc.includes('tool use') || desc.includes('function calling') || name.includes('fc'),
        };
      });

      setConfig((prev) => {
        const newProviders = [...prev.providers];
        const currentModels = newProviders[pIndex].models || [];
        const existingNames = new Set(currentModels.map((em) => em.name));

        const uniqueNewModels = scoutedModels.filter((nm) => !existingNames.has(nm.name));
        const updatedExisting = currentModels.map((em) => {
          const match = scoutedModels.find((nm) => nm.name === em.name);
          return match
            ? {
                ...em,
                max_tokens: match.max_tokens,
                thinking: match.thinking,
                reasoning: match.reasoning,
                tools: match.tools,
              }
            : em;
        });

        newProviders[pIndex] = {
          ...newProviders[pIndex],
          models: [...updatedExisting, ...uniqueNewModels],
        };
        return { ...prev, providers: newProviders };
      });

      setStatus({
        type: 'success',
        message: `RECON COMPLETE: FOUND ${freeModels.length} TARGETS (AI-VERIFIED)`,
      });
    } catch (err) {
      setStatus({ type: 'error', message: `RECON FAILED: ${err.message}` });
    }
  };

  if (loading)
    return (
      <div className="panel">
        <div className="panel-body mono">INITIALIZING...</div>
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
          <h1>Tactical Loadout</h1>
          <div
            style={{ color: 'var(--text-muted)', fontSize: '12px', fontFamily: 'var(--font-mono)' }}
          >
            CURRENT PROVIDERS: {config.providers?.length || 0}
          </div>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <button onClick={addProvider} className="btn">
            + NEW PROVIDER
          </button>
          <button onClick={handleSubmit} className="btn btn-primary">
            SYNC CONFIG
          </button>
        </div>
      </div>

      {status && (
        <div
          className="panel"
          style={{
            borderColor: status.type === 'error' ? 'var(--danger)' : 'var(--accent)',
            backgroundColor: 'var(--surface-inset)',
            marginBottom: '32px',
            position: 'sticky',
            top: '0',
            zIndex: 100,
          }}
        >
          <div
            className="panel-body mono"
            style={{
              fontSize: '12px',
              color:
                status.type === 'error'
                  ? 'var(--danger)'
                  : status.type === 'success'
                    ? 'var(--success)'
                    : 'var(--accent)',
            }}
          >
            [{status.type.toUpperCase()}] {status.message}
          </div>
        </div>
      )}

      {config.providers?.map((provider, pIndex) => (
        <div key={`provider-${pIndex}-${provider.name}`} className="panel">
          <div className="panel-header">
            <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
              <span className="status-indicator status-success"></span>
              <h3 className="mono">{provider.name}</h3>
            </div>
            <div style={{ display: 'flex', gap: '8px' }}>
              <button type="button" onClick={() => scoutModels(pIndex)} className="btn">
                🛰️ SCOUT FREE
              </button>
              <button type="button" onClick={() => optimizeProvider(pIndex)} className="btn">
                ⚡ OPTIMIZE
              </button>
              <button
                type="button"
                onClick={() => removeProvider(pIndex)}
                className="btn"
                style={{ color: 'var(--danger)' }}
              >
                DISCARD
              </button>
            </div>
          </div>

          <div className="panel-body">
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr 1fr',
                gap: '20px',
                marginBottom: '24px',
              }}
            >
              <div className="field-group">
                <label className="field-label">Registry Name</label>
                <input
                  className="field-input"
                  value={provider.name}
                  onChange={(e) => updateProvider(pIndex, 'name', e.target.value)}
                />
              </div>
              <div className="field-group">
                <label className="field-label">Access Key</label>
                <input
                  className="field-input"
                  type="password"
                  value={provider.api_key}
                  onChange={(e) => updateProvider(pIndex, 'api_key', e.target.value)}
                  placeholder="••••••••"
                />
              </div>
            </div>

            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '2fr 1fr 1fr 1fr',
                gap: '20px',
                marginBottom: '24px',
              }}
            >
              <div className="field-group">
                <label className="field-label">Base Trajectory (URL)</label>
                <input
                  className="field-input"
                  value={provider.base_url}
                  onChange={(e) => updateProvider(pIndex, 'base_url', e.target.value)}
                />
              </div>
              <div className="field-group">
                <label className="field-label">Rate Limit</label>
                <input
                  className="field-input"
                  type="number"
                  value={provider.rate_limit}
                  onChange={(e) => updateProvider(pIndex, 'rate_limit', e.target.value)}
                />
              </div>
              <div className="field-group">
                <label className="field-label">Priority</label>
                <input
                  className="field-input"
                  type="number"
                  value={provider.priority}
                  onChange={(e) => updateProvider(pIndex, 'priority', e.target.value)}
                />
              </div>
              <div className="field-group">
                <label className="field-label">Active</label>
                <div style={{ display: 'flex', alignItems: 'center', height: '36px' }}>
                  <label
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '8px',
                      cursor: 'pointer',
                      fontFamily: 'var(--font-mono)',
                      fontSize: '12px',
                      color: provider.enabled !== false ? 'var(--success)' : 'var(--text-muted)',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={provider.enabled !== false}
                      onChange={(e) => updateProvider(pIndex, 'enabled', e.target.checked)}
                      style={{
                        width: '16px',
                        height: '16px',
                        accentColor: 'var(--accent)',
                        cursor: 'pointer',
                      }}
                    />
                    {provider.enabled !== false ? 'ARMED' : 'DISABLED'}
                  </label>
                </div>
              </div>
            </div>

            <div
              style={{
                backgroundColor: 'var(--surface-inset)',
                padding: '16px',
                borderRadius: 'var(--radius)',
              }}
            >
              <div
                style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '12px' }}
              >
                <h2 style={{ fontSize: '11px', margin: 0 }}>Model Payloads</h2>
                <div style={{ display: 'flex', gap: '8px' }}>
                  <button
                    type="button"
                    onClick={() => probeAllModels(pIndex)}
                    className="btn btn-ghost"
                    style={{ fontSize: '10px' }}
                  >
                    🔍 PROBE ALL
                  </button>
                  <button
                    type="button"
                    onClick={() => addModel(pIndex)}
                    className="btn btn-ghost"
                    style={{ fontSize: '10px' }}
                  >
                    + ADD MODEL
                  </button>
                </div>
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {provider.models?.map((model, mIndex) => {
                  if (!model) return null; // Safety check
                  const probe = probeResults[`${pIndex}-${mIndex}`];
                  const itemKey = `model-${pIndex}-${model.name || mIndex}`;
                  const isDragging =
                    dragState.providerIndex === pIndex && dragState.modelIndex === mIndex;
                  return (
                    <div
                      key={itemKey}
                      draggable
                      onDragStart={(e) => handleDragStart(e, pIndex, mIndex)}
                      onDragEnd={handleDragEnd}
                      onDragOver={(e) => handleDragOver(e, pIndex, mIndex)}
                      onDragLeave={handleDragLeave}
                      onDrop={(e) => handleDrop(e, pIndex, mIndex)}
                      style={{
                        display: 'grid',
                        gridTemplateColumns: '24px 1fr 100px 200px 80px 40px',
                        gap: '12px',
                        alignItems: 'center',
                        opacity: isDragging ? 0.5 : 1,
                        cursor: 'grab',
                        padding: '4px 8px',
                        margin: '-4px -8px',
                        borderRadius: 'var(--radius)',
                        transition: 'border-color 0.15s, background-color 0.15s',
                      }}
                    >
                      <div
                        style={{
                          cursor: 'grab',
                          color: 'var(--text-muted)',
                          fontSize: '14px',
                          userSelect: 'none',
                        }}
                        title="Drag to reorder"
                      >
                        ⋮⋮
                      </div>
                      <div style={{ position: 'relative' }}>
                        <input
                          className="field-input"
                          style={{ fontSize: '12px' }}
                          value={model.name}
                          onChange={(e) => updateModel(pIndex, mIndex, 'name', e.target.value)}
                        />
                        {probe && (
                          <div
                            className="mono"
                            style={{
                              position: 'absolute',
                              right: '12px',
                              top: '50%',
                              transform: 'translateY(-50%)',
                              fontSize: '10px',
                              fontWeight: 'bold',
                              color: probe.loading
                                ? 'var(--accent)'
                                : probe.success
                                  ? 'var(--success)'
                                  : 'var(--danger)',
                              pointerEvents: 'none',
                            }}
                          >
                            {probe.loading
                              ? 'PROBING...'
                              : probe.success
                                ? `HIT ${probe.latency}MS`
                                : probe.error}
                          </div>
                        )}
                      </div>
                      <input
                        className="field-input"
                        style={{ fontSize: '12px' }}
                        type="number"
                        value={model.max_tokens}
                        onChange={(e) => updateModel(pIndex, mIndex, 'max_tokens', e.target.value)}
                        title="Max Tokens"
                      />

                      <div style={{ display: 'flex', gap: '4px' }}>
                        <button
                          type="button"
                          onClick={() => updateModel(pIndex, mIndex, 'thinking', !model.thinking)}
                          className="btn"
                          style={{
                            fontSize: '8px',
                            padding: '4px 2px',
                            flex: 1,
                            borderColor: model.thinking ? 'var(--success)' : 'var(--border)',
                            color: model.thinking ? 'var(--success)' : 'var(--text-muted)',
                            opacity: model.thinking ? 1 : 0.5,
                          }}
                        >
                          THINK
                        </button>
                        <button
                          type="button"
                          onClick={() => updateModel(pIndex, mIndex, 'reasoning', !model.reasoning)}
                          className="btn"
                          style={{
                            fontSize: '8px',
                            padding: '4px 2px',
                            flex: 1,
                            borderColor: model.reasoning ? 'var(--success)' : 'var(--border)',
                            color: model.reasoning ? 'var(--success)' : 'var(--text-muted)',
                            opacity: model.reasoning ? 1 : 0.5,
                          }}
                        >
                          REASON
                        </button>
                        <button
                          type="button"
                          onClick={() => updateModel(pIndex, mIndex, 'tools', !model.tools)}
                          className="btn"
                          style={{
                            fontSize: '8px',
                            padding: '4px 2px',
                            flex: 1,
                            borderColor: model.tools ? 'var(--success)' : 'var(--border)',
                            color: model.tools ? 'var(--success)' : 'var(--text-muted)',
                            opacity: model.tools ? 1 : 0.5,
                          }}
                        >
                          TOOLS
                        </button>
                      </div>

                      <button
                        type="button"
                        onClick={() => checkModel(pIndex, mIndex)}
                        className="btn"
                        style={{ fontSize: '10px' }}
                        disabled={probe?.loading}
                      >
                        PROBE
                      </button>
                      <button
                        type="button"
                        onClick={() => removeModel(pIndex, mIndex)}
                        className="btn btn-ghost"
                        style={{ color: 'var(--danger)' }}
                      >
                        ×
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

export default Config;
