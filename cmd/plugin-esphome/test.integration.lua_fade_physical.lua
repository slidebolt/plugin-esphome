local brightness = 0

Automation("esphome_physical_fade", {
  trigger = Interval(0.2),
  targets = None(),
}, function(ctx)
  ctx.targets:each(function(e)
    ctx.send(e, "light_set_brightness", {brightness = brightness})
  end)
  brightness = brightness + 50
  if brightness > 254 then brightness = 254 end
end)
