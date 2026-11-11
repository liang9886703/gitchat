import http from 'node:http';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, writeFile, rm, access } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const listen = server => new Promise(resolve => server.listen(0, '127.0.0.1', () => resolve(server.address().port)));
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

export async function startService() {
  const directory = await mkdtemp(join(tmpdir(), 'gitchat-browser-'));
  const attempts = new Map();
  const model = http.createServer(async (request, response) => {
    let text = '';
    for await (const chunk of request) text += chunk;
    const input = JSON.parse(text);
    const question = input.messages.at(-1)?.content || '';
    const count = (attempts.get(question) || 0) + 1;
    attempts.set(question, count);
    if (question.includes('[slow]')) await delay(3000);
    if (response.destroyed) return;
    if (question.includes('[fail]') || question.includes('[once]') && count === 1) {
      response.writeHead(503, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ error: { message: 'Controlled test model failure' } }));
      return;
    }
    let answer = `围绕「${question}」，先明确目标，再比较约束和可选方案。\n\n**独立探索**可以保留不同方向。选定一个方向深入，然后合并各分支比较。\n\n- 明确期望结果\n- 保留尚待验证的问题`;
    if (question.includes('[long]')) {
      answer += '\n\n' + Array.from({ length: 14 }, (_, i) => `第 ${i + 1} 个观察点，分别记录这个方案的条件、成本、风险与验证方式，并保留各条分支的原始结论。`).join('\n\n');
      answer += '\n\n```go\nfor _, option := range options {\n    compare(option)\n}\n```';
    }
    const result = { title: question.slice(0, 38), answer,
      suggestions: [{ title: '继续深入', question: `进一步分析 ${question.slice(0, 20)}`, score: 0.95 }, { title: '比较另一个方向', question: `比较 ${question.slice(0, 20)} 的替代方案`, score: 0.8 }, { title: '确认约束', question: '有哪些关键约束？', score: 0.7 }],
      memory: { goal: question, progress: ['明确当前问题'], branch_ideas: ['比较可选方向'], preferences: [] } };
    const content = question.includes('[invalid]') ? 'this is not JSON' : JSON.stringify(result);
    response.writeHead(200, { 'Content-Type': 'text/event-stream' });
    for (let index = 0; index < content.length; index += 37) {
      response.write(`data: ${JSON.stringify({ choices: [{ delta: { content: content.slice(index, index + 37) } }] })}\n\n`);
    }
    response.end('data: {"choices":[{"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n');
  });
  const modelPort = await listen(model);
  const portProbe = http.createServer();
  const appPort = await listen(portProbe);
  await new Promise(resolve => portProbe.close(resolve));
  const configPath = join(directory, 'config.yaml');
  await writeFile(configPath, `mode: openai\ndataDir: ${join(directory, 'data')}\nlistenAddr: 127.0.0.1:${appPort}\nrouting:\n  retries: 0\n  maxAnswerChars: 3000\n`);
  const binary = process.env.GITCHAT_TEST_BINARY || join(directory, 'gitchat');
  if (!process.env.GITCHAT_TEST_BINARY) execFileSync('go', ['build', '-o', binary, '.'], { stdio: 'inherit' });
  const child = spawn(binary, [], { env: { ...process.env, GIN_MODE: 'release', GITCHAT_CONFIG: configPath,
    GITCHAT_MODE: 'openai', GITCHAT_ADDR: `127.0.0.1:${appPort}`, DATA_DIR: join(directory, 'data'),
    OPENAI_API_KEY: 'local-test-only', OPENAI_API_KEY_0: 'local-test-only', OPENAI_BASE_URL: `http://127.0.0.1:${modelPort}/v1`, OPENAI_MODEL: 'test-model' }, stdio: ['ignore', 'pipe', 'pipe'] });
  let logs = '';
  child.stdout.on('data', data => { logs += data; });
  child.stderr.on('data', data => { logs += data; });
  const baseURL = `http://127.0.0.1:${appPort}`;
  let ready = false;
  for (let i = 0; i < 80; i++) {
    if (child.exitCode !== null) throw new Error(`Application exited during startup\n${logs}`);
    try { if ((await fetch(`${baseURL}/health`)).ok) { ready = true; break; } } catch {}
    await delay(100);
  }
  if (!ready) throw new Error(`Application did not become ready\n${logs}`);
  return { baseURL, async close() {
    child.kill('SIGTERM');
    await Promise.race([new Promise(resolve => child.once('exit', resolve)), delay(5000)]);
    if (child.exitCode === null) child.kill('SIGKILL');
    model.closeAllConnections();
    await new Promise(resolve => model.close(resolve));
    await rm(directory, { recursive: true, force: true });
  }};
}

export async function browserOptions() {
  const candidates = [process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE, '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'].filter(Boolean);
  for (const executablePath of candidates) {
    try { await access(executablePath); return { executablePath, headless: process.env.HEADED !== '1' }; } catch {}
  }
  return { headless: process.env.HEADED !== '1' };
}

if (process.argv[1] === resolve('tests/browser/fixture.mjs')) {
  const service = await startService();
  console.log(JSON.stringify({ baseURL: service.baseURL }));
  for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, async () => { await service.close(); process.exit(0); });
}
