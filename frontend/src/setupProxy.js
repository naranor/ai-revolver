const { createProxyMiddleware } = require('http-proxy-middleware');

module.exports = function (app) {
  app.use(
    '/api',
    createProxyMiddleware({
      target: 'http://localhost:8081',
      changeOrigin: true,
      // Disable compression for streaming
      onProxyReq(proxyReq, req, res) {
        proxyReq.setHeader('Accept-Encoding', 'identity');
      },
      onProxyRes(proxyRes, req, res) {
        // Remove compression headers from upstream response
        delete proxyRes.headers['content-encoding'];
        delete proxyRes.headers['content-length'];
      },
    })
  );
};
