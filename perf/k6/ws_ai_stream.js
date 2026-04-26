import { Counter, Rate, Trend } from "k6/metrics";
import { sleep } from "k6";
import { login, openWS } from "./common.js";

const aiStartOK = new Rate("ai_start_ok");
const aiDoneOK = new Rate("ai_done_ok");
const aiChunkCount = new Counter("ai_chunk_count");
const firstChunkLatency = new Trend("ai_first_chunk_ms");
const totalStreamLatency = new Trend("ai_total_stream_ms");

export const options = {
  scenarios: {
    ai_stream: {
      executor: "constant-vus",
      vus: Number(__ENV.VUS || 2), // 强烈建议不要超过 2 个并发，保护钱包和防止 429
      duration: __ENV.DURATION || "30s",
    },
  },
  thresholds: {
    ai_start_ok: ["rate>0.99"],
    ai_done_ok: ["rate>0.90"],
    ai_first_chunk_ms: ["p(95)<3000"], // AI首字延迟 < 3秒
  },
};

export function setup() {
  const vus = Number(__ENV.VUS || 2);
  const userPool = [];

  console.log(`[Setup] 准备 ${vus} 个 AI 测试账号...`);
  for (let i = 0; i < vus; i++) {
    const senderId = 3 + i;
    const senderName = String.fromCharCode(97 + i);
    const token = login(senderId, senderName, "123456");
    userPool.push({ token: token });
  }
  return { userPool };
}

export default function (data) {
  const myData = data.userPool[__VU - 1];
  const t0 = Date.now();
  let sent = false;
  let gotDone = false;
  let firstChunkTs = 0;

  openWS(myData.token, function (socket) {
    socket.on("open", function () {
      socket.send(
          JSON.stringify({
            chat_type: "single_chat", // 确保这里是你网关分发给 AI 的类型，比如 "ai_chat"
            receiver: -1,
            message: `概括一下Golang的Goroutine机制`, // 问一个具体的问题，而不是乱码，防止大模型拦截
          }),
      );
      sent = true;
    });

    socket.on("message", function (msg) {
      let obj;
      try {
        obj = JSON.parse(msg);
      } catch (_) {
        return;
      }
      if (!obj || !obj.chat_type) return;

      // 根据你后端的真实结构调整！
      if (obj.chat_type === "ai_chunk") {
        aiChunkCount.add(1);
        if (!firstChunkTs) {
          firstChunkTs = Date.now();
          firstChunkLatency.add(firstChunkTs - t0); // 精准捕获 TTFT！
        }
      } else if (obj.chat_type === "ai_end") {
        gotDone = true;
        totalStreamLatency.add(Date.now() - t0);
        socket.close(); // 收到完整的结束符，立刻撤退
      }
    });

    socket.setTimeout(function () {
      if (!gotDone) {
        console.log(`[超时] VU-${__VU} 等待AI响应超时！`);
        totalStreamLatency.add(Date.now() - t0);
      }
      socket.close();
    }, Number(__ENV.AI_WAIT_MS || 20000)); // 给大模型 20 秒的时间吐字
  });

  aiStartOK.add(sent);
  aiDoneOK.add(gotDone);

  // 必须加上较长的 sleep！防止下一轮迭代瞬间触发你的 Redis 防刷屏限流！
  sleep(Number(__ENV.SLEEP_S || 5));
}