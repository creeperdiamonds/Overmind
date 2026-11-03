// A fake Java 26.1 player. Usage: node bot.js <host> <port> <name>
// Exits 0 once the server has finished configuration, 1 on any error.
const mc = require('minecraft-protocol')

const [host = '127.0.0.1', port = '25565', username = 'OvermindBot'] = process.argv.slice(2)
const client = mc.createClient({ host, port: Number(port), username, version: '26.1', auth: 'offline' })

// The library answers the server's known packs with none; a real client
// has the vanilla core pack, so say that instead.
const write = client.write.bind(client)
client.write = (name, params) => name === 'select_known_packs'
  ? write(name, { packs: [{ namespace: 'minecraft', id: 'core', version: '26.1' }] })
  : write(name, params)

const timer = setTimeout(() => fail('timed out'), 20000)

function fail (msg) {
  console.error('bot failed: ' + msg)
  process.exit(1)
}

client.on('error', (e) => fail(e.message))
client.on('kick_disconnect', (p) => fail('kicked: ' + JSON.stringify(p)))
client.on('disconnect', (p) => fail('disconnected: ' + JSON.stringify(p)))
client.on('end', (reason) => fail('connection ended: ' + reason))
client.on('state', (s) => console.log('state ' + s))
client.on('registry_data', (p) => console.log('registry ' + p.id))
client.on('finish_configuration', () => {
  console.log('configuration finished')
})
client.on('state', (s) => {
  if (s === 'play') {
    clearTimeout(timer)
    console.log('reached play')
    process.exit(0)
  }
})
