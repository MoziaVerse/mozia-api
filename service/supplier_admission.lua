-- All supplier capacity and scheduling keys live on the existing single Redis
-- primary. Validate everything before changing reservations; no SQL in this path.
local prefix = 'supplier-routing:'
local nowParts = redis.call('TIME')
local now = tonumber(nowParts[1]) * 1000 + math.floor(tonumber(nowParts[2]) / 1000)
local op = ARGV[1]
local input = cjson.decode(ARGV[2])
local attemptKey = prefix .. 'attempt:' .. input.id

local capacityCache = {}
local function limitsState(key)
    if capacityCache[key] then return unpack(capacityCache[key]) end
    local expired = redis.call('ZRANGEBYSCORE', key .. ':rate', '-inf', now - 60000)
    for _, id in ipairs(expired) do
        local value = tonumber(redis.call('HGET', key .. ':tokens', id) or '0')
        redis.call('DECRBY', key .. ':token_total', value)
        redis.call('HDEL', key .. ':tokens', id)
    end
    redis.call('ZREMRANGEBYSCORE', key .. ':rate', '-inf', now - 60000)
    redis.call('ZREMRANGEBYSCORE', key .. ':active', '-inf', now)
    local tokens = tonumber(redis.call('GET', key .. ':token_total') or '0')
    capacityCache[key] = {redis.call('ZCARD', key .. ':active'), redis.call('ZCARD', key .. ':rate'), tokens}
    return unpack(capacityCache[key])
end

if op == 'finish' then
    local raw = redis.call('GET', attemptKey)
    if not raw then return 0 end
    local a = cjson.decode(raw)
    if a.finished then return 0 end
    for _, key in ipairs(a.keys) do
        if input.release then redis.call('ZREM', key .. ':active', input.id) end
        local reserved = tonumber(redis.call('HGET', key .. ':tokens', input.id) or '0')
        if input.cancel then
            redis.call('ZREM', key .. ':rate', input.id)
            redis.call('HDEL', key .. ':tokens', input.id)
            redis.call('DECRBY', key .. ':token_total', reserved)
        elseif input.tokens >= 0 and redis.call('ZSCORE', key .. ':rate', input.id) then
            redis.call('HSET', key .. ':tokens', input.id, input.tokens)
            redis.call('INCRBY', key .. ':token_total', input.tokens - reserved)
        end
    end
    if not input.cancel and a.health_generation == tonumber(redis.call('HGET', a.health_key, 'generation') or '0') and redis.call('HGET', a.health_key, 'state') ~= 'paused' then
        local h = a.health
        local hk = a.health_key
        local start = tonumber(redis.call('HGET', hk, 'start') or '0')
        if now - start >= h.window_seconds * 1000 then
            redis.call('HSET', hk, 'start', now, 'samples', 0, 'bad', 0, 'slow', 0)
        end
        if input.class == 'success' or input.class == 'failure' or input.class == 'overload' then
            local n = redis.call('HINCRBY', hk, 'samples', 1)
            if input.class ~= 'success' then redis.call('HINCRBY', hk, 'bad', 1) end
            if input.ttft > h.max_ttft_ms then redis.call('HINCRBY', hk, 'slow', 1) end
            local bad = tonumber(redis.call('HGET', hk, 'bad') or '0')
            local slow = tonumber(redis.call('HGET', hk, 'slow') or '0')
            if n >= h.min_samples then
                local scale = tonumber(redis.call('HGET', hk, 'scale') or h.trial_percent)
                if bad * 100 >= n * h.failure_percent then
                    redis.call('HSET', hk, 'state', 'paused', 'until', now + h.cooldown_seconds * 1000, 'scale', h.trial_percent)
                    redis.call('HINCRBY', hk, 'generation', 1)
                elseif slow * 100 >= n * h.failure_percent then
                    redis.call('HSET', hk, 'state', 'reduced', 'scale', math.max(h.trial_percent, math.floor(scale / 2)))
                elseif bad == 0 and slow == 0 then
                    scale = math.min(100, scale + h.trial_percent)
                    redis.call('HSET', hk, 'state', scale == 100 and 'normal' or 'trial', 'scale', scale)
                end
                redis.call('HSET', hk, 'start', now, 'samples', 0, 'bad', 0, 'slow', 0)
            end
        end
        redis.call('HSET', hk, 'last', now, 'ttft_ms', input.ttft)
        redis.call('EXPIRE', hk, 86400)
    end
    if input.cancel and a.share_key then
        redis.call('HINCRBY', a.share_key, 'total', -1)
        redis.call('HINCRBY', a.share_key, tostring(a.supplier_id), -1)
    end
    a.finished = true
    redis.call('SET', attemptKey, cjson.encode(a), 'EX', 86400)
    return 1
end

local existing = redis.call('GET', attemptKey)
if existing then return existing end
local raw = redis.call('GET', KEYS[1])
if not raw then return redis.error_reply('supplier routing state unavailable') end
local live = cjson.decode(raw)
if live.blocked then return redis.error_reply('supplier routing publication in progress') end
local candidates = {}
local shareKey = prefix .. 'share:' .. (input.share_scope or input.schedule) .. ':' .. math.floor(now / 3600000)
local shareTotal = tonumber(redis.call('HGET', shareKey, 'total') or '0')
local bestPriority = nil
for _, c in ipairs(input.candidates) do
    local pool = live.pools[tostring(c.pool_id)]
    local binding = live.bindings[tostring(c.channel_id) .. ':' .. input.model]
    if pool and pool.enabled and live.suppliers[tostring(pool.supplier_id)] and binding == c.pool_id then
        local spec = nil
        for _, s in ipairs(pool.models) do if s.name == input.model then spec = s end end
        local hk = prefix .. 'health:' .. c.pool_id .. ':' .. string.len(input.model) .. ':' .. input.model .. ':' .. input.group
        local state = redis.call('HGET', hk, 'state') or 'trial'
        local scale = tonumber(redis.call('HGET', hk, 'scale') or input.health.trial_percent)
        local last = tonumber(redis.call('HGET', hk, 'last') or '0')
        local untilTime = tonumber(redis.call('HGET', hk, 'until') or '0')
        if state == 'paused' and now >= untilTime then
            state = 'trial'; scale = input.health.trial_percent
            redis.call('HSET', hk, 'state', state, 'scale', scale, 'samples', 0, 'bad', 0, 'slow', 0, 'start', now)
        elseif state ~= 'paused' and now - last > input.health.window_seconds * 1000 then
            state = 'trial'; scale = input.health.trial_percent
            redis.call('HSET', hk, 'state', state, 'scale', scale, 'samples', 0, 'bad', 0, 'slow', 0, 'start', now, 'last', now)
            redis.call('HINCRBY', hk, 'generation', 1)
        end
        local keys = {prefix .. 'pool:' .. c.pool_id, prefix .. 'model:' .. c.pool_id .. ':' .. input.model}
        local fits = spec ~= nil and state ~= 'paused'
        local cap = input.max_supplier_percent or 0
        if input.first and cap > 0 then
            local count = tonumber(redis.call('HGET', shareKey, tostring(c.supplier_id)) or '0')
            if count + 1 > math.ceil((shareTotal + 1) * cap / 100) then fits = false end
        end
        local utilization = 0
        if fits then
            for i, key in ipairs(keys) do
                local lim = pool.limits
                if i == 2 then
                    lim = {}
                    for _, field in ipairs({'concurrency', 'rpm', 'tpm'}) do
                        local value = spec.limits[field]
                        if value == 0 then value = pool.limits[field] end
                        -- Limit trial concurrency, never make one valid request exceed trial TPM.
                        lim[field] = field == 'concurrency' and math.max(1, math.floor(value * scale / 100)) or value
                    end
                end
                local active, rpm, tokens = limitsState(key)
                if active + 1 > lim.concurrency or rpm + 1 > lim.rpm or tokens + c.tokens > lim.tpm then fits = false end
                utilization = math.max(utilization, (active + 1) / lim.concurrency, (rpm + 1) / lim.rpm, (tokens + c.tokens) / lim.tpm)
            end
        end
        if fits and (not bestPriority or c.priority >= bestPriority) then
            if not bestPriority or c.priority > bestPriority then candidates = {}; bestPriority = c.priority end
            c.keys = keys; c.utilization = utilization; c.health_key = hk
            c.health = input.health; c.scale = scale; c.health_generation = tonumber(redis.call('HGET', hk, 'generation') or '0')
            c.expires = now + (pool.max_execution_seconds + input.timeout_seconds) * 1000
            table.insert(candidates, c)
        end
    end
end
if #candidates == 0 then return '' end

-- One entry per supplier in the outer schedule, regardless of its channel count.
local suppliers = {}
for _, c in ipairs(candidates) do
    local sid = tostring(c.supplier_id)
    if not suppliers[sid] or c.utilization < suppliers[sid].utilization then suppliers[sid] = c end
end
local scheduleKey = prefix .. 'schedule:' .. input.schedule
local selected = nil
local total = 0
local highest = nil
local weights = {}
for sid, c in pairs(suppliers) do
    local weight = math.max(1, math.floor(c.weight * c.scale / 100))
    local score = tonumber(redis.call('HGET', scheduleKey, sid) or '0') + weight
    weights[sid] = score; total = total + weight
    if input.mode == 'share' then
        if not selected or score > highest or (score == highest and c.supplier_id < selected.supplier_id) then selected = c; highest = score end
    elseif not selected or c.utilization < selected.utilization or (c.utilization == selected.utilization and (score > highest or (score == highest and c.supplier_id < selected.supplier_id))) then
        selected = c; highest = score
    end
end
if op == 'preview' then return cjson.encode(selected) end
-- Expelled/recovering suppliers do not accumulate historical share debt.
redis.call('DEL', scheduleKey)
for sid, score in pairs(weights) do
    if sid == tostring(selected.supplier_id) then score = score - total end
    redis.call('HSET', scheduleKey, sid, score)
end
redis.call('EXPIRE', scheduleKey, 7200)
for _, key in ipairs(selected.keys) do
    redis.call('ZADD', key .. ':active', selected.expires, input.id)
    redis.call('ZADD', key .. ':rate', now, input.id)
    redis.call('HSET', key .. ':tokens', input.id, selected.tokens)
    redis.call('INCRBY', key .. ':token_total', selected.tokens)
    redis.call('EXPIRE', key .. ':token_total', 86400)
    redis.call('EXPIRE', key .. ':active', 86400)
    redis.call('EXPIRE', key .. ':rate', 86400)
    redis.call('EXPIRE', key .. ':tokens', 86400)
end
if input.first then
    selected.share_key = shareKey
    redis.call('HINCRBY', shareKey, 'total', 1)
    redis.call('HINCRBY', shareKey, tostring(selected.supplier_id), 1)
    redis.call('EXPIRE', shareKey, 7200)
end
redis.call('SET', attemptKey, cjson.encode(selected), 'EX', 86400)
return cjson.encode(selected)
