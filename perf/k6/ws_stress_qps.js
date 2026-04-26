import { Counter, Rate, Trend } from "k6/metrics";
import { sleep } from "k6";
import { login, openWS } from "./common.js"; // 确保你的 common.js 路径正确

const sendCount = new Counter("qps_msg_sent");
const recvCount = new Counter("qps_msg_recv");
const successRate = new Rate("qps_success_rate");
const e2eLatency = new Trend("qps_e2e_ms");

export const options = {
    scenarios: {
        stress_qps: {
            executor: "constant-vus",
            vus: Number(__ENV.VUS || 100), // 直接上 100 个长连接
            duration: __ENV.DURATION || "30s",
        },
    },
};

export function setup() {
    const vus = Number(__ENV.VUS || 100);
    const userPool = [];

    console.log(`[Setup] 准备打满长连接！正在为 ${vus} 个 VU 提取测试账号...`);

    for (let i = 0; i < vus; i++) {
        const senderOffset = i * 2 + 1;
        const receiverOffset = i * 2 + 2;

        const senderId = 1000 + senderOffset;
        const receiverId = 1000 + receiverOffset;

        const senderName = "test_user_" + senderId;
        const receiverName = "test_user_" + receiverId;

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
    const myPair = data.userPool[__VU - 1];

    // 用一个极其轻量的 Map 来记录每条消息的发包时间，用于计算延迟
    const pendingMsgs = new Map();
    let localSent = 0;
    let localRecv = 0;

    // 核心参数：每个 VU 多久发一条消息？
    // 如果是 50ms，1 个 VU 每秒发 20 条。100 个 VU 就是 2000 QPS 目标！
    const sendIntervalMs = Number(__ENV.SEND_INTERVAL || 50);

    openWS(myPair.senderToken, function (socket) {
        let intervalId;

        socket.on("open", function () {
            console.log(`[VU-${__VU}] 连接成功，开始疯狂倾泻消息...`);

            // 开启无情连发模式！
            intervalId = socket.setInterval(function () {
                const now = Date.now();
                // 用时间戳和自增序号作为唯一 MsgID
                const msgId = `msg_${__VU}_${localSent}_${now}`;

                // 把发送时间存起来
                pendingMsgs.set(msgId, now);

                const packet = JSON.stringify({
                    chat_type: "single_chat",
                    receiver: myPair.senderId, // 依然是极致的 Echo 模式：自己发给自己
                    message: msgId,
                });

                socket.send(packet);
                localSent++;
                sendCount.add(1);
            }, sendIntervalMs);
        });

        socket.on("message", function (msg) {
            localRecv++;
            recvCount.add(1);

            // 【高阶技巧】不在这里做复杂的 JSON 解析，因为 k6 的 JS 引擎遇到几万 QPS 的 JSON 解析会卡死！
            // 我们直接用正则或简单的字符串匹配把 msgId 抠出来算延迟
            const match = msg.match(/msg_\d+_\d+_(\d+)/);
            if (match && match[1]) {
                const sentTime = parseInt(match[1], 10);
                e2eLatency.add(Date.now() - sentTime);
            }
        });

        // 彻底关闭连接 (关闭时，k6 会自动清理 interval 定时器)
        socket.setTimeout(function () {
            socket.close();
        }, Number(__ENV.DURATION_MS || 28000));
    });

    // 记录这一个 VU 的整体存活率
    if (localSent > 0) {
        successRate.add(localRecv / localSent);
    }
}