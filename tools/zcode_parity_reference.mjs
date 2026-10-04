// 开发测试使用 Node 标准库生成合成向量，生产 Go 模块不依赖 Node。
// 来源：TriDefender/zcode-api src/proxy/client-signing.ts。
// 固定 commit：0f8788ae02d3a2432b96276f200e7c41bf8f64d7；归属与 MIT 声明见 NOTICE.md。
import { createHash, createHmac, hkdfSync, createCipheriv, createPrivateKey, sign } from 'node:crypto'

const id = 'synthetic-id'
const secret = 'synthetic-secret'
const ts = '1700000000123'
const nonce = '00112233445566778899aabbccddeeff'
const session = 'synthetic-session'
const version = '3.14.0'
const derive = info => Buffer.from(hkdfSync('sha256', Buffer.from(secret), Buffer.from('WD_CLIENT_SIGN_KDF_SALT'), Buffer.from(info), 32))
const seed = Buffer.from(Array.from({ length: 32 }, (_, i) => i))
const pkcs8 = Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), seed])
const aesNonce = Buffer.from('000102030405060708090a0b', 'hex')
const aes = createCipheriv('aes-256-gcm', derive('ed25519_priv'), aesNonce)
aes.setAAD(Buffer.from(id))
const encrypted = Buffer.concat([aes.update(pkcs8.toString('base64'), 'utf8'), aes.final()])
const privateCipher = Buffer.concat([aesNonce, encrypted, aes.getAuthTag()]).toString('base64')
const message = [id, ts, version, session, nonce].join('\n')
const signature = sign(null, Buffer.from(message), createPrivateKey({ key: pkcs8, format: 'der', type: 'pkcs8' })).toString('base64')
const powSeed = createHash('sha256').update([id, 'zcode', session, ts].join('\n')).digest('hex').slice(0, 32)
let pow
for (let i = 0; i < 0xffffffff; i++) {
  const candidate = '00112233445566778899aabb' + i.toString(16).padStart(8, '0')
  if (createHash('sha256').update(powSeed + '\n' + candidate).digest()[0] === 0) { pow = candidate; break }
}
console.log(JSON.stringify({ upstream_commit: '0f8788ae02d3a2432b96276f200e7c41bf8f64d7', id, secret, ts, nonce, session, version, seed_hex: seed.toString('hex'), hkdf_private_hex: derive('ed25519_priv').toString('hex'), handshake_hmac: createHmac('sha256', derive('getSignKey_hmac')).update(['get_sign_key', id, ts, nonce].join('\n')).digest('base64'), private_cipher: privateCipher, message, signature, pow_seed: powSeed, pow }, null, 2))
