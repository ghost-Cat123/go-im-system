import { Counter, Rate, Trend } from "k6/metrics";
import { sleep } from "k6";
import { login, mustEnv, openWS } from "./common.js";

const sendOK = new Rate("online_send_ok");
const receiveOK = new Rate("online_receive_ok");
const e2eLatency = new Trend("online_e2e_ms");
const recvCount = new Counter("online_recv_count");

export const options = {
  scenarios: {
    online_chat: {
      executor: "constant-vus",
      vus: Number(__ENV.VUS || 10), // 假设 10 个 VU
      duration: __ENV.DURATION || "1m",
    },
  },
  thresholds: {
    online_send_ok: ["rate>0.99"],
    online_receive_ok: ["rate>0.99"],
    online_e2e_ms: ["p(95)<500"],
  },
};

export function setup() {
  const vus = Number(__ENV.VUS || 10); // 正好使用 10 个并发用户
  const userPool = [];

  console.log(`[Setup] 正在为 ${vus} 个 VU 登录专属测试账号 (从 1001号/a 到 1201号/t)...`);

  // 你有 20 个用户，分成 10 组，绝对隔离！
  for (let i = 0; i < vus; i++) {
    // i=0 时: sender 是 3(a), receiver 是 4(b)
    // i=1 时: sender 是 5(c), receiver 是 6(d)
    const senderOffset = i * 2 + 1;
    const receiverOffset = i * 2 + 2;

    const senderId = 1000 + senderOffset;
    const receiverId = 1000 + receiverOffset;

    // ASCII 码 97 就是小写字母 'a'
    const senderName = "test_user_" + senderId;
    const receiverName = "test_user_" + receiverId;

    // 使用真实的密码 "123456" 登录
    const senderToken = login(senderId, senderName, "123456");
    const receiverToken = login(receiverId, receiverName, "123456");

    // ... 前面的 login 逻辑不变 ...
    userPool.push({
      senderId: senderId,           // <--- 加上这一行！把发送者的 ID 明确存下来
      senderToken: senderToken,
      receiverId: receiverId,
      receiverToken: receiverToken,
    });
  }
  return { userPool };
}

export default function (data) {
  const myPair = data.userPool[__VU - 1]; // 每个 VU 拿到自己的专属对

  let sentAt = 0;
  let gotReply = false;
  let sendSucceed = false;

  // 极简主义：只用发送者的 Token 开一个连接！自己发，自己收！
  openWS(myPair.senderToken, function (socket) {

    // 连上后立刻发送
    socket.on("open", function () {
      const body = `k6-online-${__VU}-${__ITER}-${Date.now()}`;
      const packet = JSON.stringify({
        chat_type: "single_chat",
        receiver: myPair.senderId,  // <--- 修改这里！正确使用发送者自己的 ID
        message: body,
      });
      sentAt = Date.now();
      socket.send(packet);
      sendSucceed = true;
    });

    // 收到消息后立刻计算延迟并关闭
    socket.on("message", function (msg) {
      gotReply = true;
      recvCount.add(1);
      if (sentAt > 0) {
        e2eLatency.add(Date.now() - sentAt); // 这次绝对能算出真实的延迟！
      }
      socket.close(); // 收到就撤，进入下一次迭代
    });

    // 兜底超时
    socket.setTimeout(function () {
      socket.close();
    }, Number(__ENV.WAIT_MS || 3000));
  });

  sendOK.add(sendSucceed);
  receiveOK.add(gotReply);
  sleep(Number(__ENV.SLEEP_S || 0.2));
}