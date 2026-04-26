import { Counter, Rate, Trend } from "k6/metrics";
import { sleep } from "k6";
import { login, mustEnv, openWS } from "./common.js";

const syncRate = new Rate("offline_sync_ok");
const syncCount = new Counter("offline_sync_count");
const syncLatency = new Trend("offline_sync_ms");

export const options = {
  scenarios: {
    offline_sync: {
      executor: "constant-vus",
      vus: Number(__ENV.VUS || 10), // 使用 10 个 VU
      duration: __ENV.DURATION || "1m",
    },
  },
  thresholds: {
    offline_sync_ok: ["rate>0.95"],
    offline_sync_ms: ["p(95)<3000"],
  },
};

export function setup() {
  const vus = Number(__ENV.VUS || 10);
  const userPool = [];

  console.log(`[Setup] 正在为 ${vus} 个 VU 登录专属测试账号 (用于离线测试)...`);

  // 复用我们上一轮的账号池逻辑 (3-22号, a-t)
  for (let i = 0; i < vus; i++) {
    const senderOffset = i * 2;
    const receiverOffset = i * 2 + 1;

    const senderId = 3 + senderOffset;
    const receiverId = 3 + receiverOffset;

    const senderName = String.fromCharCode(97 + senderOffset);
    const receiverName = String.fromCharCode(97 + receiverOffset);

    const senderToken = login(senderId, senderName, "123456");
    const receiverToken = login(receiverId, receiverName, "123456");

    userPool.push({
      senderId: senderId,
      senderToken: senderToken,
      receiverId: receiverId,
      receiverToken: receiverToken,
    });
  }
  return { userPool };
}

export default function (data) {
  const myPair = data.userPool[__VU - 1]; // 获取专属账号对
  const burst = Number(__ENV.OFFLINE_BURST || 10);
  let received = 0;
  const t0 = Date.now();

  // Phase-1: Receiver 此时是不在线的，Sender 上线疯狂发消息
  openWS(myPair.senderToken, function (socket) {
    socket.on("open", function () {
      for (let i = 0; i < burst; i += 1) {
        socket.send(
            JSON.stringify({
              chat_type: "single_chat",
              receiver: myPair.receiverId, // <--- 发给专属的 Receiver
              message: `k6-offline-${__VU}-${__ITER}-${i}`,
            }),
        );
      }
    });
    // 发完就跑，留出充足时间让 Redis/MySQL 消化落库
    socket.setTimeout(function () {
      socket.close();
    }, Number(__ENV.SENDER_WAIT_MS || 1500));
  });

  // 停顿一下，模拟真实的离线时间差
  sleep(Number(__ENV.BETWEEN_PHASE_S || 0.5));

  // Phase-2: Receiver 上线！期待服务器主动下发或者通过发送 Sync 协议拉取
  openWS(myPair.receiverToken, function (socket) {
    socket.on("message", function (msg) {
      let obj;
      try {
        obj = JSON.parse(msg);
      } catch (_) {
        return;
      }

      // 注意：这里取决于你的后端架构，离线消息是变成 single_chat 下发，还是统一打成一个 sync_unread 数组？
      // 如果后端是一条条发，这里逻辑是对的；如果是发一个大数组，这里 received += obj.messages.length 更好
      if (obj && (obj.chat_type === "sync_unread" || obj.chat_type === "single_chat")) {
        received += 1;
        syncCount.add(1);
      }

      if (received >= burst) {
        syncLatency.add(Date.now() - t0);
        socket.close(); // 收齐了就完美闭环下线
      }
    });

    socket.setTimeout(function () {
      if (received < burst) {
        syncLatency.add(Date.now() - t0); // 超时了还没收齐
      }
      socket.close();
    }, Number(__ENV.RECEIVER_WAIT_MS || 5000));
  });

  syncRate.add(received >= burst);
  sleep(Number(__ENV.SLEEP_S || 0.5));
}