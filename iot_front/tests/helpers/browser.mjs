import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

export { delay }

// Each check owns a fresh browser process and profile; never attach to a user's browser.
export async function startBrowser({ args = [], timeout = 10000, interval = 100, onEvent = () => {}, webSocketImplementation = WebSocket } = {}) {
  const executable = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
  const profile = await mkdtemp(join(tmpdir(), 'iot-browser-test-'))
  const mode = process.env.IOT_TEST_HEADFUL === '1' ? ['--window-position=-4000,-4000', '--window-size=1440,900'] : ['--headless=new']
  const pending = new Map(), errors = [], warnings = []
  let child, socket, exited, failure, sequence = 0

  function fail(error) {
    failure ||= error
    for (const request of pending.values()) request.reject(failure)
    pending.clear()
  }

  async function until(check, note = '', limit = timeout) {
    const deadline = Date.now() + limit
    do {
      if (failure) throw failure
      const value = await check()
      if (value) return value
      await delay(interval)
    } while (Date.now() < deadline)
    throw new Error(`Browser condition timed out${note ? `: ${note}` : ''}`)
  }

  async function close() {
    fail(new Error('Browser session closed'))
    socket?.close()
    if (child?.pid && child.exitCode === null && child.signalCode === null) {
      child.kill()
      const forceStop = setTimeout(() => child.kill('SIGKILL'), 3000)
      forceStop.unref()
      try { await exited } finally { clearTimeout(forceStop) }
    }
    await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
  }

  function call(method, params = {}) {
    return new Promise((resolve, reject) => {
      if (failure) return reject(failure)
      const id = ++sequence
      const timer = setTimeout(() => {
        pending.delete(id)
        reject(new Error(`CDP timeout: ${method}`))
      }, 30000)
      pending.set(id, {
        resolve: value => { clearTimeout(timer); resolve(value) },
        reject: error => { clearTimeout(timer); reject(error) },
      })
      try { socket.send(JSON.stringify({ id, method, params })) }
      catch (error) { pending.get(id).reject(error); pending.delete(id) }
    })
  }

  async function evaluate(expression) {
    const value = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })
    if (value.exceptionDetails) throw new Error(value.exceptionDetails.exception?.description || value.exceptionDetails.text)
    return value.result.value
  }

  try {
    child = spawn(executable, [...mode, '--no-first-run', '--no-default-browser-check', '--disable-gpu', ...args,
      '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
    exited = new Promise(resolve => {
      child.once('error', error => { fail(error); resolve() })
      child.once('exit', (code, signal) => { fail(new Error(`Browser exited (code=${code}, signal=${signal})`)); resolve() })
    })
    const port = await until(async () => {
      try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] }
      catch (error) { if (error.code !== 'ENOENT') throw error }
    }, 'DevToolsActivePort')
    const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`, { signal: AbortSignal.timeout(10000) })).json()
    socket = new webSocketImplementation(pages.find(page => page.type === 'page').webSocketDebuggerUrl)
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('Browser debugger connection timed out')), 10000)
      socket.onopen = () => { clearTimeout(timer); resolve() }
      socket.onerror = () => { clearTimeout(timer); reject(new Error('Browser debugger connection failed')) }
      socket.onclose = () => { clearTimeout(timer); reject(new Error('Browser debugger connection closed')) }
    })
    socket.onclose = event => fail(new Error(`Browser debugger connection closed (code=${event.code}, reason=${event.reason || 'unspecified'})`))
    socket.onerror = () => fail(new Error('Browser debugger connection failed'))
    socket.onmessage = event => {
      const message = JSON.parse(event.data)
      if (message.id) {
        const request = pending.get(message.id)
        if (!request) return // A response may arrive after its timeout.
        pending.delete(message.id)
        message.error ? request.reject(new Error(message.error.message)) : request.resolve(message.result)
        return
      }
      if (message.method === 'Runtime.exceptionThrown') errors.push(message.params.exceptionDetails.exception?.description || message.params.exceptionDetails.text)
      if (message.method === 'Runtime.consoleAPICalled' && ['warning', 'error'].includes(message.params.type)) warnings.push(message.params.args.map(arg => arg.value || arg.description || '').join(' '))
      if (['Inspector.detached', 'Inspector.targetCrashed'].includes(message.method)) fail(new Error(`Browser page disconnected: ${message.params.reason || message.method}`))
      onEvent(message)
    }
    return { call, evaluate, until, errors, warnings, close }
  } catch (error) {
    await close()
    throw error
  }
}
