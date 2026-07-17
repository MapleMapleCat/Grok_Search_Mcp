const registrationProofWorkerURL = new URL("./workers/registration-proof-worker.js", import.meta.url);

export function solveRegistrationProof({ challenge, difficulty, expiresAt, username, inviteCode = "" }) {
  return new Promise((resolve, reject) => {
    const expirationTimestamp = new Date(expiresAt).getTime();
    if (!challenge || !Number.isInteger(difficulty) || difficulty < 1 || difficulty > 32) {
      reject(new Error("服务端返回了无效的注册计算任务。"));
      return;
    }
    if (!Number.isFinite(expirationTimestamp) || expirationTimestamp <= Date.now()) {
      reject(new Error("注册计算任务已过期，请重试。"));
      return;
    }

    const proofWorker = new Worker(registrationProofWorkerURL, { type: "module" });
    const expirationDelay = Math.max(1, expirationTimestamp - Date.now());
    const expirationTimer = window.setTimeout(() => {
      proofWorker.terminate();
      reject(new Error("注册计算任务已过期，请重试。"));
    }, expirationDelay);

    function finish(callback, value) {
      window.clearTimeout(expirationTimer);
      proofWorker.terminate();
      callback(value);
    }

    proofWorker.addEventListener("message", (event) => {
      if (event.data?.type === "solved" && typeof event.data.nonce === "string") {
        finish(resolve, { challenge, nonce: event.data.nonce });
        return;
      }
      finish(reject, new Error(event.data?.message || "无法完成注册计算任务。"));
    }, { once: true });

    proofWorker.addEventListener("error", () => {
      finish(reject, new Error("浏览器无法完成注册计算任务。"));
    }, { once: true });

    proofWorker.postMessage({
      challenge,
      difficulty,
      username,
      inviteCode
    });
  });
}
