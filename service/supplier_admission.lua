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


-- ponytail: five minute buckets bound Redis work per candidate; use histograms
-- only if request-length cohorts and tail-latency routing become necessary.
local function performanceMetrics(key, generation)
    local m = {success=0, failure=0, overload=0, ttft=0, ttft_n=0, tps=0, tps_n=0}
    local minute = math.floor(now / 60000)
    for i = 0, 4 do
        local values = redis.call('HGETALL', key .. ':g' .. generation .. ':' .. (minute-i))
        for j=1,#values,2 do
            if m[values[j]] ~= nil then m[values[j]] = m[values[j]] + tonumber(values[j+1]) end
        end
    end
    m.samples = m.success + m.failure
    return m
end

local function performanceTier(c, h)
    local key = c.performance_key
    local state = redis.call('HGET', key, 'state') or 'trial'
    if tonumber(redis.call('HGET', key, 'until') or '0') > now then return nil end
    c.performance_generation = tonumber(redis.call('HGET', key, 'generation') or '0')
    local m = performanceMetrics(key, c.performance_generation)
    if m.samples < h.min_samples or m.tps_n < h.min_samples or (c.streaming and m.ttft_n < h.min_samples) then return 2 end
    if m.failure * 100 >= m.samples * h.failure_percent then return nil end
    if m.success * 100 < m.samples * h.success_percent or m.tps/m.tps_n < h.min_throughput or (c.streaming and m.ttft/m.ttft_n > h.max_ttft_ms) then return 1 end
    if state == 'degraded' and tonumber(redis.call('HGET', key, 'good_periods') or '0') < 2 then return 1 end
    return 0
end

local function finishPerformance(a)
    if not a.performance_key or not a.measure or input.cancel or (input.class ~= 'success' and input.class ~= 'failure' and input.class ~= 'overload') then return end
    local key = a.performance_key
    local loadKey = key .. ':load:' .. math.floor(now/60000)
    redis.call('HINCRBY', loadKey, 'arrivals', 1)
    if input.class == 'overload' then redis.call('HINCRBY', loadKey, 'overload_arrivals', 1) end
    redis.call('EXPIRE', loadKey, 360)
    local generation = tonumber(redis.call('HGET', key, 'generation') or '0')
    if generation ~= (a.performance_generation or 0) then return end
    local minute = math.floor(now / 60000)
    local bucket = key .. ':g' .. generation .. ':' .. minute
    redis.call('HINCRBY', bucket, input.class, 1)
    if input.class == 'success' then
        redis.call('HSET', key, 'consecutive_failures', 0)
        if input.ttft > 0 then
            redis.call('HINCRBYFLOAT', bucket, 'ttft', input.ttft)
            redis.call('HINCRBY', bucket, 'ttft_n', 1)
        end
        if input.tokens >= 0 and (input.latency or 0) > 0 then
            redis.call('HINCRBYFLOAT', bucket, 'tps', input.output * 1000/input.latency)
            redis.call('HINCRBY', bucket, 'tps_n', 1)
            local outputKey = a.output_scope .. ':' .. minute
            redis.call('HINCRBY', outputKey, 'output', input.output)
            redis.call('HINCRBY', outputKey, 'count', 1)
            redis.call('EXPIRE', outputKey, 360)
        end
    end
    redis.call('EXPIRE', bucket, 360)
    redis.call('HSET', key, 'last', now)
    redis.call('EXPIRE', key, 86400)
    if not a.adaptive then return end
    local h = a.health
    local m = performanceMetrics(key, generation)
    local consecutive = tonumber(redis.call('HGET', key, 'consecutive_failures') or '0')
    if input.class == 'failure' then consecutive = redis.call('HINCRBY', key, 'consecutive_failures', 1) end
    if input.class == 'overload' or consecutive >= 3 or (m.samples >= h.min_samples and m.failure*100 >= m.samples*h.failure_percent) then
        local delay = h.cooldown_seconds
        local state = 'paused'
        if input.class == 'overload' then state = 'overloaded'; delay = math.max(delay, math.min(86400, input.retry_after or 0)) end
        redis.call('HSET', key, 'state', state, 'until', now+delay*1000, 'consecutive_failures', 0, 'good_periods', 0)
        redis.call('HINCRBY', key, 'generation', 1)
        return
    end
    local tier = performanceTier(a, h)
    if tier == 2 then redis.call('HSET', key, 'state', 'trial'); return end
    local good = m.samples >= h.min_samples and m.success*100 >= m.samples*h.success_percent and m.tps_n >= h.min_samples and m.tps/m.tps_n >= h.min_throughput and (not a.streaming or (m.ttft_n >= h.min_samples and m.ttft/m.ttft_n <= h.max_ttft_ms))
    if not good then redis.call('HSET', key, 'state', 'degraded', 'good_periods', 0); return end
    if redis.call('HGET', key, 'state') == 'degraded' then
        local prior = tonumber(redis.call('HGET', key, 'good_minute') or '0')
        if prior ~= minute then
            local count = prior == minute-1 and tonumber(redis.call('HGET', key, 'good_periods') or '0')+1 or 1
            redis.call('HSET', key, 'good_minute', minute, 'good_periods', count)
            if count < 2 then return end
        elseif tonumber(redis.call('HGET', key, 'good_periods') or '0') < 2 then return end
    end
    redis.call('HSET', key, 'state', 'normal', 'until', 0)
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
    if input.release and a.trial_key then redis.call('ZREM', a.trial_key, input.id) end
    finishPerformance(a)
    if not a.adaptive and not input.cancel and a.health_generation == tonumber(redis.call('HGET', a.health_key, 'generation') or '0') and redis.call('HGET', a.health_key, 'state') ~= 'paused' then
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
if input.mode == 'adaptive' and input.revision ~= live.config.revision then return redis.error_reply('supplier routing revision changed; retry the request') end
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
        local adaptive = input.mode == 'adaptive'
        local state = redis.call('HGET', hk, 'state') or 'trial'
        local scale = tonumber(redis.call('HGET', hk, 'scale') or input.health.trial_percent)
        local last = tonumber(redis.call('HGET', hk, 'last') or '0')
        local untilTime = tonumber(redis.call('HGET', hk, 'until') or '0')
        if not adaptive and state == 'paused' and now >= untilTime then
            state = 'trial'; scale = input.health.trial_percent
            redis.call('HSET', hk, 'state', state, 'scale', scale, 'samples', 0, 'bad', 0, 'slow', 0, 'start', now)
        elseif not adaptive and state ~= 'paused' and now - last > input.health.window_seconds * 1000 then
            state = 'trial'; scale = input.health.trial_percent
            redis.call('HSET', hk, 'state', state, 'scale', scale, 'samples', 0, 'bad', 0, 'slow', 0, 'start', now, 'last', now)
            redis.call('HINCRBY', hk, 'generation', 1)
        end
        local keys = {prefix .. 'pool:' .. c.pool_id, prefix .. 'model:' .. c.pool_id .. ':' .. input.model}
        local fits = spec ~= nil and (adaptive or state ~= 'paused')
        c.adaptive = adaptive
        if c.performance_key then c.performance_generation = tonumber(redis.call('HGET', c.performance_key, 'generation') or '0') end
        if adaptive then
            c.tier = performanceTier(c, input.health)
            fits = fits and c.tier ~= nil
            if c.tier == 2 then
                c.trial_key = prefix .. 'trial:' .. c.pool_id
                redis.call('ZREMRANGEBYSCORE', c.trial_key, '-inf', now)
                if redis.call('ZCARD', c.trial_key) >= input.health.trial_concurrency then fits = false end
            end
        end
        local cap = input.max_supplier_percent or 0
        if not adaptive and input.first and cap > 0 then
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
                        lim[field] = not adaptive and value > 0 and field == 'concurrency' and math.max(1, math.floor(value * scale / 100)) or value
                    end
                end
                local active, rpm, tokens = limitsState(key)
                for field, used in pairs({concurrency=active+1, rpm=rpm+1, tpm=tokens+c.tokens}) do
                    if lim[field] > 0 then
                        if used > lim[field] then fits = false end
                        utilization = math.max(utilization, used / lim[field])
                    end
                end
            end
        end
        if fits and (adaptive or not bestPriority or c.priority >= bestPriority) then
            if not adaptive and (not bestPriority or c.priority > bestPriority) then candidates = {}; bestPriority = c.priority end
            c.keys = keys; c.utilization = utilization; c.health_key = hk
            c.health = input.health; c.scale = scale; c.health_generation = tonumber(redis.call('HGET', hk, 'generation') or '0')
            c.expires = now + (pool.max_execution_seconds + input.timeout_seconds) * 1000
            table.insert(candidates, c)
        end
    end
end
if #candidates == 0 then return '' end

local adaptive = input.mode == 'adaptive'
local suppliers = {}
local bestTier = 2
local channelTies = {}
for _, c in ipairs(candidates) do
    local sid = tostring(adaptive and c.pool_id or c.supplier_id)
    local prior = suppliers[sid]
    if not prior or (adaptive and (c.cost < prior.cost or (c.cost == prior.cost and c.channel_id < prior.channel_id))) or (not adaptive and c.utilization < prior.utilization) then suppliers[sid] = c end
    if adaptive then
        if not channelTies[sid] or c.cost < channelTies[sid][1].cost then channelTies[sid] = {c}
        elseif c.cost == channelTies[sid][1].cost then table.insert(channelTies[sid], c) end
        if c.tier < bestTier then bestTier = c.tier end
    end
end
local scheduleKey = prefix .. 'schedule:' .. input.schedule
local explorationKey = scheduleKey .. ':exploration'
local explore = false
if adaptive then
    local step = tonumber(redis.call('GET', explorationKey) or '0') + 1
    explore = bestTier == 2 or step % math.ceil(100/input.health.trial_percent) == 0
    local hasTrial = false
    for _, c in pairs(suppliers) do if c.tier == 2 then hasTrial = true end end
    explore = explore and hasTrial
    for sid, c in pairs(suppliers) do
        if (explore and c.tier ~= 2) or (not explore and c.tier ~= bestTier) then suppliers[sid] = nil end
    end
    if explore then scheduleKey = scheduleKey .. ':trial' end
end
local minimumCost = nil
if adaptive then for _, c in pairs(suppliers) do if not minimumCost or c.cost < minimumCost then minimumCost = c.cost end end end
local selected = nil
local total = 0
local highest = nil
local weights = {}
for sid, c in pairs(suppliers) do
    local weight = math.max(1, math.floor(c.weight * c.scale / 100))
    if adaptive then
        weight = 1
        if not explore and minimumCost > 0 then weight = math.max(1, math.floor(1000*(minimumCost/c.cost)^2+0.5)) end
        if not explore and minimumCost == 0 and c.cost > 0 then weight = 0 end
    end
    if weight > 0 then
        c.routing_weight = weight
        if adaptive then c.health_state = c.tier == 0 and 'normal' or (c.tier == 1 and 'degraded' or 'trial') end
        local score = tonumber(redis.call('HGET', scheduleKey, sid) or '0') + weight
        weights[sid] = score; total = total + weight
        if adaptive or input.mode == 'share' then
            if not selected or score > highest or (score == highest and c.channel_id < selected.channel_id) then selected = c; highest = score end
        elseif not selected or c.utilization < selected.utilization or (c.utilization == selected.utilization and (score > highest or (score == highest and c.supplier_id < selected.supplier_id))) then
            selected = c; highest = score
        end
    end
end
if adaptive then
    local tied = channelTies[tostring(selected.pool_id)]
    local rotation = prefix .. 'channel-rotation:' .. selected.pool_id .. ':' .. input.model
    local turn = tonumber(redis.call('GET', rotation) or '0')
    local chosen = tied[turn % #tied + 1]
    selected.channel_id = chosen.channel_id; selected.price_json = chosen.price_json
    if op ~= 'preview' then redis.call('INCR', rotation); redis.call('EXPIRE', rotation, 7200) end
end
if op == 'preview' then return cjson.encode(selected) end
if adaptive then
    redis.call('INCR', explorationKey); redis.call('EXPIRE', explorationKey, 7200)
    redis.call('HSET', selected.performance_key, 'state', selected.health_state)
    if selected.trial_key then redis.call('ZADD', selected.trial_key, selected.expires, input.id); redis.call('EXPIRE', selected.trial_key, 86400) end
end
-- Expelled/recovering suppliers do not accumulate historical share debt.
redis.call('DEL', scheduleKey)
for sid, score in pairs(weights) do
    if sid == tostring(adaptive and selected.pool_id or selected.supplier_id) then score = score - total end
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
