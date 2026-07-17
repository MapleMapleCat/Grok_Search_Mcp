const registrationProofPrefix = "grok-registration-pow-v1\0";
const maximumSafeCounter = Number.MAX_SAFE_INTEGER;
const twoToThe32 = 0x100000000;

const initialHashWords = new Uint32Array([
  0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
  0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
]);

const roundConstants = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
  0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
  0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
  0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
  0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
]);

self.addEventListener("message", (event) => {
  try {
    const challenge = String(event.data?.challenge || "");
    const username = String(event.data?.username || "");
    const inviteCode = String(event.data?.inviteCode || "");
    const difficulty = Number(event.data?.difficulty);
    if (!challenge || !Number.isInteger(difficulty) || difficulty < 1 || difficulty > 32) {
      throw new Error("Invalid registration proof challenge");
    }

    const nonce = solveChallenge(challenge, username, inviteCode, difficulty);
    self.postMessage({ type: "solved", nonce: String(nonce) });
  } catch (error) {
    self.postMessage({ type: "error", message: error instanceof Error ? error.message : "Proof calculation failed" });
  }
});

function solveChallenge(challenge, username, inviteCode, difficulty) {
  const textEncoder = new TextEncoder();
  const messagePrefix = textEncoder.encode(
    `${registrationProofPrefix}${challenge}\0${username}\0${inviteCode}\0`
  );
  const counterOffset = messagePrefix.length;
  const messageLength = counterOffset + 8;
  const paddedLength = Math.ceil((messageLength + 9) / 64) * 64;
  const paddedMessage = new Uint8Array(paddedLength);
  paddedMessage.set(messagePrefix);
  paddedMessage[messageLength] = 0x80;

  const paddedMessageView = new DataView(paddedMessage.buffer);
  const messageBitLength = messageLength * 8;
  paddedMessageView.setUint32(paddedLength - 8, Math.floor(messageBitLength / twoToThe32), false);
  paddedMessageView.setUint32(paddedLength - 4, messageBitLength >>> 0, false);

  const messageSchedule = new Uint32Array(64);
  for (let counter = 0; counter <= maximumSafeCounter; counter += 1) {
    writeCounter(paddedMessageView, counterOffset, counter);
    const firstDigestWord = calculateFirstDigestWord(paddedMessageView, messageSchedule);
    if (hasLeadingZeroBits(firstDigestWord, difficulty)) {
      return counter;
    }
  }
  throw new Error("Registration proof counter space exhausted");
}

function writeCounter(messageView, offset, counter) {
  const highWord = Math.floor(counter / twoToThe32);
  const lowWord = counter - (highWord * twoToThe32);
  messageView.setUint32(offset, highWord, false);
  messageView.setUint32(offset + 4, lowWord, false);
}

function calculateFirstDigestWord(messageView, messageSchedule) {
  let hash0 = initialHashWords[0];
  let hash1 = initialHashWords[1];
  let hash2 = initialHashWords[2];
  let hash3 = initialHashWords[3];
  let hash4 = initialHashWords[4];
  let hash5 = initialHashWords[5];
  let hash6 = initialHashWords[6];
  let hash7 = initialHashWords[7];

  for (let blockOffset = 0; blockOffset < messageView.byteLength; blockOffset += 64) {
    for (let wordIndex = 0; wordIndex < 16; wordIndex += 1) {
      messageSchedule[wordIndex] = messageView.getUint32(blockOffset + (wordIndex * 4), false);
    }
    for (let wordIndex = 16; wordIndex < 64; wordIndex += 1) {
      const previousWord15 = messageSchedule[wordIndex - 15];
      const previousWord2 = messageSchedule[wordIndex - 2];
      const sigma0 = rotateRight(previousWord15, 7) ^ rotateRight(previousWord15, 18) ^ (previousWord15 >>> 3);
      const sigma1 = rotateRight(previousWord2, 17) ^ rotateRight(previousWord2, 19) ^ (previousWord2 >>> 10);
      messageSchedule[wordIndex] = (
        messageSchedule[wordIndex - 16]
        + sigma0
        + messageSchedule[wordIndex - 7]
        + sigma1
      ) >>> 0;
    }

    let working0 = hash0;
    let working1 = hash1;
    let working2 = hash2;
    let working3 = hash3;
    let working4 = hash4;
    let working5 = hash5;
    let working6 = hash6;
    let working7 = hash7;

    for (let roundIndex = 0; roundIndex < 64; roundIndex += 1) {
      const upperSigma1 = rotateRight(working4, 6) ^ rotateRight(working4, 11) ^ rotateRight(working4, 25);
      const choose = (working4 & working5) ^ (~working4 & working6);
      const temporary1 = (working7 + upperSigma1 + choose + roundConstants[roundIndex] + messageSchedule[roundIndex]) >>> 0;
      const upperSigma0 = rotateRight(working0, 2) ^ rotateRight(working0, 13) ^ rotateRight(working0, 22);
      const majority = (working0 & working1) ^ (working0 & working2) ^ (working1 & working2);
      const temporary2 = (upperSigma0 + majority) >>> 0;

      working7 = working6;
      working6 = working5;
      working5 = working4;
      working4 = (working3 + temporary1) >>> 0;
      working3 = working2;
      working2 = working1;
      working1 = working0;
      working0 = (temporary1 + temporary2) >>> 0;
    }

    hash0 = (hash0 + working0) >>> 0;
    hash1 = (hash1 + working1) >>> 0;
    hash2 = (hash2 + working2) >>> 0;
    hash3 = (hash3 + working3) >>> 0;
    hash4 = (hash4 + working4) >>> 0;
    hash5 = (hash5 + working5) >>> 0;
    hash6 = (hash6 + working6) >>> 0;
    hash7 = (hash7 + working7) >>> 0;
  }

  return hash0;
}

function hasLeadingZeroBits(firstDigestWord, difficulty) {
  if (difficulty === 32) {
    return firstDigestWord === 0;
  }
  return (firstDigestWord >>> (32 - difficulty)) === 0;
}

function rotateRight(value, bitCount) {
  return (value >>> bitCount) | (value << (32 - bitCount));
}
