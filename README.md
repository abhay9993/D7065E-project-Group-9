# Smart HVAC Optimisation (Group 9)

This repo holds our D7065E project: a smart HVAC system for a simulated office building,
built on top of BuildSim.

The idea is simple. Rooms fill up and empty out during the day. Instead of running
heating, cooling, and ventilation on a fixed schedule, we want the system to look at
occupancy, temperature, and humidity in each room and decide when to actually turn
things up or down. When a meeting room fills up, it should get more cooling or
ventilation. When it empties, it should drop into a low-energy mode. The goal is to
keep people comfortable without wasting energy.

See the project proposal for the full write-up of the use case, scenario, and plan.

## What's in here

- `services/simulator` - generates the physical behaviour behind the sensors: room
  temperature from a heat balance model, humidity, and occupancy patterns through the day.
- `services/sensors/temperature`, `services/sensors/humidity`, `services/sensors/occupancy` -
  one process per sensor type, per room.
- `services/actuators/hvac` - controls heating, cooling, and fan speed per room and
  reports state back to BuildSim.
- `services/controller` - the autonomous decision-maker. Watches sensor data and decides
  when to change HVAC settings.
- `services/pipeline` - collects sensor readings and keeps recent history around for the
  controller and the dashboard.
- `services/dashboard` - shows room state, occupancy, and what the controller has been
  doing.
- `docs/` - diagrams, API notes, anything that doesn't belong in code.

Each service is meant to run as its own process (containerized with Docker) and talk to
the others through a defined interface, not by importing each other's code. BuildSim
provides the building itself and the API we plug into.

