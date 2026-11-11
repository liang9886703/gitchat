import http from 'node:http';

// A real failing network boundary avoids Playwright WebKit's offline-emulation
// bug that rejects even literal Service Worker responses (#42775 upstream).
// All normal requests still reach the real Go application, including SSE.
export async function networkGateway(target) {
  const backend = new URL(target);
  let available = true;
  const server = http.createServer((request, response) => {
    if (!available) { request.socket.destroy(); return; }
    const upstream = http.request({hostname:backend.hostname,port:backend.port,path:request.url,method:request.method,headers:request.headers}, incoming => {
      response.writeHead(incoming.statusCode, incoming.headers);
      incoming.pipe(response);
    });
    upstream.on('error', () => { if (!response.headersSent) response.writeHead(502); response.end(); });
    request.on('aborted',()=>upstream.destroy());
    response.on('close',()=> {if(!response.writableFinished) upstream.destroy();});
    request.pipe(upstream);
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  return {
    baseURL:`http://127.0.0.1:${server.address().port}`,
    setAvailable(value) {available=value;if(!value)server.closeAllConnections();},
    async close() {server.closeAllConnections();await new Promise(resolve=>server.close(resolve));},
  };
}
