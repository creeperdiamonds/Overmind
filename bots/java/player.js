// A fake Java 26.1 player that stays online, for internal/javafront tests.
// Usage: node player.js <host> <port> <name>
//
// Reads one JSON command per line on stdin and answers with one JSON line on
// stdout carrying the same "id". It also prints events: {"ev": "play"},
// {"ev": "kick", "reason": ...}, {"ev": "end", "reason": ...}.
//
// Chunks are decoded with prismarine-chunk, not by anything of ours, so a wrong
// encoder on the server shows up here as wrong blocks.
const readline = require('readline')
const mc = require('minecraft-protocol')
const Vec3 = require('vec3').Vec3
const registry = require('prismarine-registry')('26.1')
const Chunk = require('prismarine-chunk')(registry)
const Block = require('prismarine-block')(registry) // comes with prismarine-chunk
const mcData = require('minecraft-data')('26.1')
const nbt = require('prismarine-nbt')

const [host, port, username] = process.argv.slice(2)
const quiet = process.env.BOT_QUIET === '1' // memtest: walk, skip the decoding
const client = mc.createClient({ host, port: Number(port), username, version: '26.1', auth: 'offline' })

const write = client.write.bind(client)
client.write = (name, params) => name === 'select_known_packs'
  ? write(name, { packs: [{ namespace: 'minecraft', id: 'core', version: '26.1' }] })
  : write(name, params)

const say = (o) => process.stdout.write(JSON.stringify(o) + '\n')

const me = { x: 0, y: 0, z: 0, yaw: 0, pitch: 0, onGround: false, known: false }
const seen = { login: null, spawn: null, keepAlives: 0, batches: 0, packets: {}, health: null, respawns: 0 }
const chunks = new Map()
const heightmaps = new Map()
const names = new Map() // uuid to name
const tab = new Map() // the player list: uuid to name
const profiles = new Map() // name to the profile properties its row carried
const chat = [] // every chat message, in order
const inv = new Array(46).fill(null) // window 0, as the server last said
let cursor = null
let invState = 0
let held = 0
const equipment = new Map() // entity id to { slot number: item }
const entities = new Map() // entity id to what we know
const blocks = new Map() // "x,y,z" to the last block_change
const states = new Map() // "x,y,z" to the last block state ID sent for it
const acks = []
const collects = [] // every collect: { collected, collector, count }
const selfMetadata = {} // our own entity's metadata, by index
const selfVelocities = [] // every velocity sent for our own entity
const playerType = registry.entitiesByName.player.id
let walk = null
let target = null // a point to walk to
let climbTo = null

const ckey = (x, z) => x + ',' + z

for (const name of ['game_state_change', 'update_view_position', 'map_chunk', 'unload_chunk', 'player_info', 'spawn_entity', 'entity_destroy', 'block_change', 'position']) {
  client.on(name, () => { seen.packets[name] = (seen.packets[name] || 0) + 1 })
}

client.on('update_health', (p) => { seen.health = p.health })
// The dimension the client is in, from Login and Respawn: its chunks are as
// tall as its dimension type (the Nether and the End 0 to 256).
const heights = { "minecraft:the_nether": [0, 256], "minecraft:the_end": [0, 256] }
let dim = { name: "minecraft:overworld", minY: -64, height: 384 }
const enter = (ws) => { const h = heights[ws.name] || [-64, 384]; dim = { name: ws.name, minY: h[0], height: h[1] }; seen.dimension = ws.name; seen.dimensionType = ws.dimension }
client.on('respawn', (p) => {
  seen.respawns++
  enter(p.worldState)
  chunks.clear(); entities.clear(); blocks.clear(); states.clear()
})
client.on('multi_block_change', (p) => {
  const c = p.chunkCoordinates
  for (const r of p.records) {
    // A record may be past 32 bits, so no bit operators on its top part.
    const state = Math.floor(r / 4096)
    const x = c.x * 16 + ((r >> 8) & 15)
    const z = c.z * 16 + ((r >> 4) & 15)
    const y = c.y * 16 + (r & 15)
    const b = registry.blocksByStateId[state]
    blocks.set(x + ',' + y + ',' + z, b ? b.name : 'unknown:' + state)
    states.set(x + ',' + y + ',' + z, state)
  }
})
client.on('login', (p) => { seen.login = p; enter(p.worldState); say({ ev: 'play' }) })
client.on('spawn_position', (p) => { seen.spawn = p })
client.on('keep_alive', () => { seen.keepAlives++ })
client.on('position', (p) => {
  Object.assign(me, { x: p.x, y: p.y, z: p.z, yaw: p.yaw, pitch: p.pitch, known: true })
  client.write('teleport_confirm', { teleportId: p.teleportId })
})
client.on('chunk_batch_finished', () => {
  seen.batches++
  client.write('chunk_batch_received', { chunksPerTick: 20 })
})
client.on('map_chunk', (p) => {
  if (quiet) return
  const c = new Chunk({ minY: dim.minY, worldHeight: dim.height })
  try {
    c.load(p.chunkData)
  } catch (e) {
    // A chunk prismarine-chunk cannot read is a failure of ours: report it.
    say({ ev: 'error', reason: 'chunk ' + p.x + ',' + p.z + ': ' + e.message })
    return
  }
  c.loadParsedLight(p.skyLight, p.blockLight, p.skyLightMask, p.blockLightMask, p.emptySkyLightMask, p.emptyBlockLightMask)
  chunks.set(ckey(p.x, p.z), c)
  heightmaps.set(ckey(p.x, p.z), p.heightmaps)
  for (const be of p.blockEntities || []) {
    blockEntities.set((p.x * 16 + be.x) + ',' + be.y + ',' + (p.z * 16 + be.z), { type: be.type, nbt: be.nbtData ? nbt.simplify(be.nbtData) : null, chunk: true })
  }
})
client.on('unload_chunk', (p) => chunks.delete(ckey(p.chunkX, p.chunkZ)))
client.on('player_info', (p) => {
  for (const e of p.data) {
    if (!e.player) continue
    names.set(e.uuid, e.player.name)
    tab.set(e.uuid, e.player.name)
    profiles.set(e.player.name, { uuid: e.uuid, properties: e.player.properties || [] })
  }
})
client.on('player_remove', (p) => p.players.forEach((u) => {
  const name = tab.get(u)
  tab.delete(u)
  if (name !== undefined && profiles.has(name) && profiles.get(name).uuid === u) profiles.delete(name)
}))
// Chat as the client gets it: components simplified from NBT to plain values.
// The command tree, as the server last declared it.
let commandTree = null
client.on('declare_commands', (p) => { commandTree = p })
client.on('system_chat', (p) => chat.push({ kind: 'system', msg: nbt.simplify(p.content) }))
client.on('profileless_chat', (p) => chat.push({ kind: 'disguised', msg: nbt.simplify(p.message), name: nbt.simplify(p.name), type: p.type }))
client.on('player_chat', (p) => chat.push({ kind: 'player', msg: p.plainMessage }))
// Items as the protocol.json parser read them, named by the item registry.
const itemOf = (s) => {
  if (!s || !s.itemCount) return null
  const it = registry.items[s.itemId]
  return { name: it ? it.name : 'unknown:' + s.itemId, count: s.itemCount, components: (s.components || []).map((c) => ({ type: c.type, data: c.data })) }
}
client.on('window_items', (p) => {
  cursor = itemOf(p.carriedItem)
  if (p.windowId !== 0) {
    win.slots = p.items.map(itemOf)
    win.state = p.stateId
    return
  }
  p.items.forEach((s, i) => { inv[i] = itemOf(s) })
  invState = p.stateId
})
client.on('set_slot', (p) => {
  if (p.windowId !== 0) {
    if (win.open && p.windowId === win.open.id) { win.slots[p.slot] = itemOf(p.item); win.state = p.stateId }
    return
  }
  inv[p.slot] = itemOf(p.item)
  invState = p.stateId
})
// The block window open, as the server said: its type in the menu registry,
// its title, slots and properties (a furnace's progress).
const win = { open: null, slots: [], state: 0, props: {}, opened: 0 }
client.on('open_window', (p) => { Object.assign(win, { open: { id: p.windowId, type: p.inventoryType, title: nbt.simplify(p.windowTitle) }, slots: [], props: {}, opened: win.opened + 1 }) })
client.on('close_window', (p) => { if (win.open && p.windowId === win.open.id) win.open = null })
client.on('craft_progress_bar', (p) => { win.props[p.property] = p.value })
// Block entities as the server sent them, in chunks and alone, by position.
const blockEntities = new Map()
client.on('tile_entity_data', (p) => { blockEntities.set(p.location.x + ',' + p.location.y + ',' + p.location.z, { type: p.action, nbt: p.nbtData ? nbt.simplify(p.nbtData) : null }) })
const signOpens = []
client.on('open_sign_entity', (p) => signOpens.push({ x: p.location.x, y: p.location.y, z: p.location.z, front: p.isFrontText }))
client.on('set_cursor_item', (p) => { cursor = itemOf(p.contents) })
client.on('held_item_slot', (p) => { held = p.slot })
client.on('entity_equipment', (p) => {
  const e = equipment.get(p.entityId) || {}
  for (const q of p.equipments) e[q.slot] = itemOf(q.item)
  equipment.set(p.entityId, e)
})
client.on('spawn_entity', (p) => {
  entities.set(p.entityId, { uuid: p.objectUUID, type: p.type, x: p.x, y: p.y, z: p.z, yaw: p.yaw * 360 / 256, pitch: p.pitch * 360 / 256, head: p.headPitch * 360 / 256, spawnVelocity: p.velocity, velocity: null, metadata: {}, data: p.objectData })
})
// Metadata as the protocol.json parser read it, by index; item stacks named.
client.on('entity_metadata', (p) => {
  const e = entities.get(p.entityId)
  const into = e ? e.metadata : (selfMetadata)
  if (!e && p.entityId !== 1) return
  for (const m of p.metadata) into[m.key] = m.type === 'item_stack' ? itemOf(m.value) : m.value
})
client.on('entity_velocity', (p) => {
  const e = entities.get(p.entityId)
  if (e) e.velocity = p.velocity
  else if (p.entityId === 1) selfVelocities.push(p.velocity)
})
// What happened to entities, by entity ID (1 is our own): statuses,
// animations and damage, and our own hurt animations and deaths.
const events = { statuses: {}, animations: {}, damage: {}, hurt: [], deaths: [] }
const note = (m, id, v) => { (m[id] = m[id] || []).push(v) }
client.on('entity_status', (p) => note(events.statuses, p.entityId, p.entityStatus))
client.on('animation', (p) => note(events.animations, p.entityId, p.animation))
client.on('damage_event', (p) => note(events.damage, p.entityId, { type: p.sourceTypeId, cause: p.sourceCauseId, direct: p.sourceDirectId }))
client.on('hurt_animation', (p) => events.hurt.push({ id: p.entityId, yaw: p.yaw }))
client.on('death_combat_event', (p) => events.deaths.push({ id: p.playerId, message: nbt.simplify(p.message) }))
client.on('collect', (p) => collects.push({ collected: p.collectedEntityId, collector: p.collectorEntityId, count: p.pickupItemCount }))
client.on('entity_destroy', (p) => p.entityIds.forEach((id) => entities.delete(id)))
const moved = (p) => {
  const e = entities.get(p.entityId)
  if (!e) return
  e.x += p.dX / 4096; e.y += p.dY / 4096; e.z += p.dZ / 4096
  if (p.yaw !== undefined) { e.yaw = p.yaw * 360 / 256; e.pitch = p.pitch * 360 / 256 }
}
client.on('rel_entity_move', moved)
client.on('entity_move_look', moved)
client.on('entity_head_rotation', (p) => {
  const e = entities.get(p.entityId)
  if (e) e.head = p.headYaw * 360 / 256
})
client.on('sync_entity_position', (p) => {
  const e = entities.get(p.entityId)
  if (e) Object.assign(e, { x: p.x, y: p.y, z: p.z, yaw: p.yaw, pitch: p.pitch })
})
client.on('block_change', (p) => {
  const b = registry.blocksByStateId[p.type]
  blocks.set(p.location.x + ',' + p.location.y + ',' + p.location.z, b ? b.name : 'unknown:' + p.type)
  states.set(p.location.x + ',' + p.location.y + ',' + p.location.z, p.type)
})
client.on('acknowledge_player_digging', (p) => acks.push(p.sequenceId))
client.on('kick_disconnect', (p) => say({ ev: 'kick', reason: JSON.stringify(p.reason) }))
// minecraft-protocol parses by protocol.json and reports a packet whose bytes do
// not fit its schema, too long or too short, here. Tests treat this as a failure.
client.on('error', (e) => say({ ev: 'error', reason: e.message }))
client.on('end', (r) => { say({ ev: 'end', reason: String(r) }); process.exit(0) })

function blockAt (x, y, z) {
  const c = chunks.get(ckey(Math.floor(x / 16), Math.floor(z / 16)))
  if (!c) return null
  const id = c.getBlockStateId(new Vec3(Math.floor(x) & 15, Math.floor(y), Math.floor(z) & 15)) // prismarine takes coordinates inside the chunk
  return registry.blocksByStateId[id].name
}

// Moves the bot one tick, as a client would: it falls until a block stops it.
setInterval(() => {
  if (!me.known) return
  if (walk) {
    me.x += Math.cos(walk.angle) * walk.speed
    me.z += Math.sin(walk.angle) * walk.speed
    me.yaw = walk.angle * 180 / Math.PI - 90
  } else if (target) {
    const dx = target.x - me.x
    const dz = target.z - me.z
    const d = Math.hypot(dx, dz)
    if (d <= 0.3) { me.x = target.x; me.z = target.z; target = null } else { me.x += dx / d * 0.3; me.z += dz / d * 0.3 }
  }
  const under = blockAt(me.x, me.y - 0.001, me.z)
  if (climbTo !== null) {
    me.y = Math.min(me.y + 5, climbTo)
    if (me.y >= climbTo) climbTo = null
  } else if (seen.health !== null && seen.health <= 0) {
    // Dead: a client stays put on its death screen.
  } else if (under === 'air') {
    const next = me.y - 0.4
    me.y = blockAt(me.x, next, me.z) === 'air' ? next : Math.floor(next) + 1
  } else {
    // Inside a solid block (a chunk that arrived after a step down): stand on
    // it, as a client's collision would put it.
    const at = blockAt(me.x, me.y, me.z)
    if (at !== null && at !== 'air' && at !== 'water' && at !== 'lava' && me.y !== Math.floor(me.y)) me.y = Math.floor(me.y) + 1
  }
  if (under !== null) me.onGround = blockAt(me.x, me.y - 0.001, me.z) !== 'air'
  client.write('position_look', { x: me.x, y: me.y, z: me.z, yaw: me.yaw, pitch: me.pitch, flags: { onGround: me.onGround, hasHorizontalCollision: false } })
  client.write('tick_end', {})
}, 50)

// heightAt reads one 9-bit column from a packed heightmap, 7 entries a long.
function heightAt (longs, x, z) {
  const i = z * 16 + x
  const raw = longs[Math.floor(i / 7)]
  const v = Array.isArray(raw) ? (BigInt.asUintN(32, BigInt(raw[0])) << 32n) | BigInt.asUintN(32, BigInt(raw[1])) : BigInt.asUintN(64, BigInt(raw))
  return Number((v >> BigInt((i % 7) * 9)) & 511n)
}

// guessUsed takes one item from a hand's window slot, as the client's guess.
function guessUsed (hand) {
  const slot = hand ? 45 : 36 + held
  const s = inv[slot]
  if (!s) return
  inv[slot] = s.count > 1 ? { ...s, count: s.count - 1 } : null
}

const toServerIDs =mcData.protocol.play.toServer.types.packet[1][0].type[1].mappings
const idOf = (name) => Number(Object.keys(toServerIDs).find((k) => toServerIDs[k] === name))
const firstUnusedID = Math.max(...Object.keys(toServerIDs).map(Number)) + 1

const commands = {
  state: () => ({ me, seen }),
  stateName: ({ state }) => ({ name: registry.blocksByStateId[state].name }),
  maxState: () => ({ state: Math.max(...registry.blocksArray.map((b) => b.maxStateId)) }),
  // column reports what prismarine-chunk decoded for one block column.
  column: ({ x, z, from, to }) => {
    const c = chunks.get(ckey(x >> 4, z >> 4))
    if (!c) return { loaded: false }
    const out = {}
    for (let y = from; y <= to; y++) {
      const p = new Vec3(x & 15, y, z & 15)
      out[y] = { block: registry.blocksByStateId[c.getBlockStateId(p)].name, biome: registry.biomes[c.getBiome(p)].name, sky: c.getSkyLight(p) }
    }
    const maps = heightmaps.get(ckey(x >> 4, z >> 4))
    return { loaded: true, blocks: out, heightmaps: maps.map((h) => h.type), height: heightAt(maps[0].data, x & 15, z & 15) }
  },
  walk: ({ angle, speed }) => { walk = { angle, speed: speed || 0.2158 }; return {} },
  stop: () => { walk = null; return {} },
  climb: ({ y }) => { climbTo = y; return {} },
  respawn: () => { client.write('client_command', { actionId: 'perform_respawn' }); return {} },
  jump: ({ dx }) => { me.x += dx; return {} },
  dig: ({ status, x, y, z, face, sequence }) => {
    client.write('block_dig', { status, location: { x, y, z }, face, sequence })
    return {}
  },
  block: ({ x, y, z }) => {
    const k = x + ',' + y + ',' + z
    return { name: blocks.get(k) || null, state: states.has(k) ? states.get(k) : null, acks }
  },
  // blockState is the block at a position as the client has it (the last
  // block_change, else the chunk), with its properties decoded by
  // prismarine-block from minecraft-data, not by anything of ours.
  blockState: ({ x, y, z }) => {
    const k = x + ',' + y + ',' + z
    let state = states.has(k) ? states.get(k) : null
    if (state === null) {
      const c = chunks.get(ckey(Math.floor(x / 16), Math.floor(z / 16)))
      if (!c) return { loaded: false }
      state = c.getBlockStateId(new Vec3(x & 15, y, z & 15))
    }
    const b = Block.fromStateId(state, 0)
    return { loaded: true, state, name: b.name, props: b.getProperties() }
  },
  // look turns the bot; the next movement packet carries it.
  look: ({ yaw, pitch }) => { me.yaw = yaw; me.pitch = pitch; return {} },
  // place uses the item in a hand on a block face (block_place), with the
  // cursor inside the block, 0 to 1.
  // With predict, the bot takes one from the hand's slot itself first, as a
  // vanilla client guesses, so only the server's answer puts it right.
  place: ({ x, y, z, face, cx, cy, cz, hand, sequence, predict }) => {
    if (predict) guessUsed(hand)
    client.write('block_place', { hand: hand ? 'off_hand' : 'main_hand', location: { x, y, z }, direction: face, cursorX: cx, cursorY: cy, cursorZ: cz, insideBlock: false, worldBorderHit: false, sequence })
    return {}
  },
  // useItem uses the item in a hand in the air (use_item), looking as the bot does.
  useItem: ({ hand, sequence, predict }) => {
    if (predict) guessUsed(hand)
    client.write('use_item', { hand: hand ? 'off_hand' : 'main_hand', sequence, rotation: { x: me.yaw, y: me.pitch } })
    return {}
  },
  // release stops using the held item (Player Action 5): a bow shoots.
  release: () => { client.write('block_dig', { status: 5, location: { x: 0, y: 0, z: 0 }, face: 0, sequence: 0 }); return {} },
  chat: () => ({ chat }),
  tab: () => ({ names: [...tab.values()] }),
  // profile is the row the player list has for a name: the UUID the client
  // knows the player by and the profile properties (textures) it carried.
  profile: ({ name }) => profiles.has(name) ? { known: true, ...profiles.get(name) } : { known: false, properties: [] },
  quit: () => { client.end('bye'); return {} },
  // say sends chat as an offline client does: unsigned, nothing acknowledged.
  say: ({ text }) => {
    client.write('chat_message', { message: text, timestamp: BigInt(Date.now()), salt: 0n, signature: undefined, offset: 0, acknowledged: Buffer.alloc(3), checksum: 0 })
    return {}
  },
  command: ({ text }) => { client.write('chat_command', { command: text }); return {} },
  inventory: () => ({ slots: inv, cursor, held, state: invState }),
  commandTree: () => ({ tree: commandTree }),
  // click sends a window click with no guesses at the result, which the
  // server works out itself.
  click: ({ slot, button, mode, window }) => {
    const id = window || 0
    client.write('window_click', { windowId: id, stateId: id ? win.state : invState, slot, mouseButton: button, mode, changedSlots: [], cursorItem: undefined })
    return {}
  },
  hold: ({ slot }) => { client.write('held_item_slot', { slotId: slot }); held = slot; return {} },
  creative: ({ slot, item, count }) => {
    const it = registry.itemsByName[item]
    client.write('set_creative_slot', { slot, item: { itemCount: count, itemId: it.id, addedComponentCount: 0, removedComponentCount: 0, components: [], removeComponents: [] } })
    return {}
  },
  // action sends a Player Action with no block: 3 drops the held stack, 4 one
  // item, 6 swaps hands.
  action: ({ status }) => { client.write('block_dig', { status, location: { x: 0, y: 0, z: 0 }, face: 0, sequence: 0 }); return {} },
  closeWindow: ({ window }) => { client.write('close_window', { windowId: window || 0 }); if (window && win.open && win.open.id === window) win.open = null; return {} },
  window: () => ({ ...win, cursor }),
  blockEntity: ({ x, y, z }) => ({ entity: blockEntities.get(x + ',' + y + ',' + z) || null }),
  signOpens: () => ({ opens: signOpens }),
  updateSign: ({ x, y, z, front, lines }) => {
    client.write('update_sign', { location: { x, y, z }, isFrontText: front, text1: lines[0] || '', text2: lines[1] || '', text3: lines[2] || '', text4: lines[3] || '' })
    return {}
  },
  equipment: ({ name }) => {
    for (const [id, e] of entities) if ((names.get(e.uuid) || e.uuid) === name) return { equipment: equipment.get(id) || {} }
    return { equipment: null }
  },
  players: () => {
    const out = {}
    for (const e of entities.values()) if (e.type === playerType) out[names.get(e.uuid) || e.uuid] = e
    return { players: out }
  },
  // entities is every shown entity by ID, with its type's name.
  entities: () => {
    const out = {}
    for (const [id, e] of entities) out[id] = { ...e, typeName: registry.entities[e.type] ? registry.entities[e.type].name : 'unknown', name: names.get(e.uuid) || null }
    return { entities: out, collects, events, self: { metadata: selfMetadata, velocities: selfVelocities } }
  },
  goto: ({ x, z }) => { walk = null; target = { x, z }; return {} },
  // input sends player_input with these keys held (forward, shift, jump...).
  input: ({ keys }) => {
    const inputs = { forward: false, backward: false, left: false, right: false, jump: false, shift: false, sprint: false }
    for (const k of keys || []) inputs[k] = true
    client.write('player_input', { inputs })
    return {}
  },
  // playerCommand sends entity_action: start_sprinting, stop_sprinting,
  // start_fall_flying...
  playerCommand: ({ action }) => { client.write('entity_action', { entityId: 1, actionId: action, jumpBoost: 0 }); return {} },
  settings: ({ skinParts }) => {
    client.write('settings', { locale: 'en_us', viewDistance: 6, chatFlags: 0, chatColors: true, skinParts, mainHand: 1, enableTextFiltering: false, enableServerListing: true, particleStatus: 'all' })
    return {}
  },
  // swing sends arm_animation, attack attacks an entity (and swings, as a
  // client does).
  swing: () => { client.write('arm_animation', { hand: 'main_hand' }); return {} },
  attack: ({ target }) => {
    client.write('attack', { entityId: target })
    client.write('arm_animation', { hand: 'main_hand' })
    return {}
  },
  junk: ({ kind }) => {
    if (kind === 'unknown-id') client.writeRaw(Buffer.from([firstUnusedID]))
    else if (kind === 'short-position') client.writeRaw(Buffer.from([idOf('position'), 1, 2, 3]))
    else if (kind === 'nan-position') {
      const b = Buffer.alloc(26)
      b[0] = idOf('position')
      b.writeDoubleBE(NaN, 1); b.writeDoubleBE(0, 9); b.writeDoubleBE(0, 17)
      client.writeRaw(b)
    } else if (kind === 'flood') {
      const b = Buffer.from([idOf('flying'), 1])
      for (let i = 0; i < 3000; i++) client.writeRaw(b)
    } else throw new Error('unknown junk ' + kind)
    return {}
  }
}

readline.createInterface({ input: process.stdin }).on('line', (line) => {
  const c = JSON.parse(line)
  try {
    say({ id: c.id, ...commands[c.cmd](c) })
  } catch (e) {
    say({ id: c.id, err: e.stack })
  }
})
process.stdin.on('end', () => process.exit(0))
