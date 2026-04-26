import http from 'k6/http'
import ws from 'k6/ws'
import { check } from 'k6'

export function mustEnv(name) {
	const v = __ENV[name]
	if (!v) {
		throw new Error(`missing env: ${name}`)
	}
	return v
}

export function gatewayBase() {
	return __ENV.GATEWAY_BASE || 'http://127.0.0.1:8080'
}

export function wsBase() {
	const base = __ENV.WS_BASE || 'ws://127.0.0.1:8080'
	return base.replace(/\/$/, '')
}

export function wsURL(token) {
	return `${wsBase()}/ws?token=${encodeURIComponent(token)}`
}

export function openWS(token, handler) {
	const url = wsURL(token)
	const res = ws.connect(url, {}, handler)
	check(res, { 'ws upgrade 101': (r) => r && r.status === 101 })
	return res
}

export function login(userId, userName, password) {
	const url = `${gatewayBase()}/api/login`
	const payload = JSON.stringify({
		user_id: userId,
		user_name: userName,
		password: password,
	})
	const res = http.post(url, payload, {
		headers: { 'Content-Type': 'application/json' },
		timeout: __ENV.HTTP_TIMEOUT || '10s',
	})
	const ok = check(res, {
		'login status 200': (r) => r.status === 200,
	})
	if (!ok) {
		throw new Error(`login failed status=${res.status} body=${res.body}`)
	}
	const data = res.json()
	if (!data || !data.data || !data.data.token) {
		throw new Error(`login response missing token: ${res.body}`)
	}
	return data.data.token
}

export function randomSuffix() {
	return `${__VU}-${__ITER}-${Date.now()}`
}
