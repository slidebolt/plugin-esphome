-- 20-second rainbow: orange → yellow → red → green → blue
-- 200 steps at 100ms each = 20 seconds total.
local step  = 0
local total = 200  -- 20s

local palette = {
    {255, 165,   0},  -- orange
    {255, 255,   0},  -- yellow
    {255,   0,   0},  -- red
    {  0, 255,   0},  -- green
    {  0,   0, 255},  -- blue
}

local transitions    = #palette - 1  -- 4
local steps_per      = total / transitions  -- 50 per transition

Automation("esphome_rainbow", {
    trigger = Interval(0.1),
    targets = None(),
}, function(ctx)
    if step >= total then return end

    local ti = math.floor(step / steps_per)
    local t  = (step % steps_per) / steps_per

    local c1 = palette[ti + 1]
    local c2 = palette[ti + 2]

    local r = math.floor(c1[1] + (c2[1] - c1[1]) * t)
    local g = math.floor(c1[2] + (c2[2] - c1[2]) * t)
    local b = math.floor(c1[3] + (c2[3] - c1[3]) * t)

    ctx.targets:each(function(e)
        ctx.send(e, "light_set_rgb", {r = r, g = g, b = b})
    end)

    step = step + 1
end)
