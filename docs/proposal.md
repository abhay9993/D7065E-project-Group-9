# Project Proposal, D7065E: Smart HVAC Optimisation

**Team:** Abhay Soni, Monika Shrivastava
**Use case:** Smart HVAC optimisation
**Pitch:** Optimise heating, cooling, and ventilation using occupancy, temperature, and
humidity data to maintain comfort while reducing energy consumption.
**Repository:** https://github.com/&lt;org-or-user&gt;/D7065E-project-Group-9

## 1. Use case, and why

This project focuses on smart HVAC optimisation in a simulated office building. The
use case was chosen because it addresses a real and important problem in modern
buildings: maintaining occupant comfort while reducing energy waste. Heating, cooling,
and ventilation are often scheduled conservatively or managed with fixed thresholds,
which can lead to inefficient operation when rooms are empty or only partially occupied.

This use case is especially relevant to the course because it combines autonomous
control with sensor-driven decision-making in a realistic cyber-physical environment.
The building simulator provides room-level data, including occupancy and environmental
conditions, which can be used to make real-time control decisions. It also lets the
project include a data-driven component, such as occupancy forecasting or thermal
prediction, without requiring a highly complex AI model.

## 2. Scenario

The target scenario is an office floor with several rooms that experience different
occupancy patterns over the course of a day. In the morning, rooms begin to fill as
employees arrive. During meetings or lunch periods, occupancy increases sharply and
the heating or cooling demand changes. At the end of the day, many rooms become empty,
and the system should reduce energy use by moving into a low-energy setback mode.

A representative end-to-end story: a meeting room becomes occupied, temperature and
humidity rise, and the system detects that the room needs additional ventilation or
cooling. The autonomous controller adapts the HVAC settings to keep the room within a
comfortable range while avoiding unnecessary energy use. Once the room is empty, the
system reduces HVAC activity and preserves energy. The same logic applies across
multiple rooms with different schedules and occupancy levels.

## 3. What will be built

- **Temperature sensors:** measure room temperature and thermal variation.
- **Humidity sensors:** monitor moisture and comfort conditions.
- **Occupancy counters:** estimate room occupancy and usage patterns over time.
- **HVAC actuators:** control heating, cooling, and fan speed per room or zone.
- **Autonomous decision-maker:** decides when to change HVAC settings based on current
  and forecasted conditions.
- **Data pipeline / storage:** collects sensor readings and stores recent history for
  analysis and model training.
- **Monitoring dashboard:** visualises room state, occupancy, and recent controller
  decisions.

Each major component runs as a separate process and communicates through defined
interfaces. The system integrates with the simulator through its API, and the
controller writes actuator commands back into the building model, closing the loop
between environmental state and control actions.

## 4. Simulating the building

BuildSim provides the rooms, but the physical processes and occupancy patterns must be
simulated as part of the project.

- **Physical processes:** temperature is generated from a heat-balance model influenced
  by outdoor conditions, body heat from occupants, HVAC action, and heat loss through
  room boundaries. Humidity is modelled as correlated with occupancy and HVAC operation.
- **People movement:** occupancy follows a realistic daily pattern with morning
  arrivals, lunch-time peaks, meeting-room usage, and evening departures, with added
  randomness so no two days are identical.
- **Data-driven component:** a simple forecasting or predictive model for occupancy or
  thermal demand informs the HVAC controller and improves energy-aware decisions.

The models are intentionally simple, but they must produce realistic trends and respond
to actuator commands so the control loop remains meaningful.

## 5. The autonomous part

The HVAC controller uses a simple data-driven thermal-demand forecast based on
occupancy and room conditions, combined with a rule-based supervisory policy to
balance comfort and energy efficiency. It monitors temperature, humidity, and occupancy
to estimate short-term thermal demand and adjusts heating, cooling, and ventilation
before comfort drops or energy is wasted. For example:

- if a room is occupied and too warm, the controller increases cooling or ventilation
- if a room is occupied and too cold, it raises the heating setpoint
- if a room is empty, it reduces HVAC activity and enters a low-energy setback mode
- if humidity exceeds a threshold, it increases ventilation

This hybrid approach keeps the system explainable and easy to validate while still
satisfying the course requirement for a data-driven component, and it's a realistic
starting point that can be refined later with more advanced forecasting or optimisation.

## 6. Biggest unknowns and risks

- whether the simplified thermal and humidity models are realistic enough to produce
  meaningful control decisions
- whether the controller can respond quickly enough to changes in occupancy and room
  conditions
- whether a small forecasting model improves performance enough to justify the added
  complexity
- whether the system can reduce energy use without compromising thermal comfort
- whether communication between components stays reliable as more rooms are added

These risks are acceptable at the proposal stage, since the point of the proposal is to
validate the direction and get approval to start building. The design is expected to
evolve as implementation progresses.

## 7. First step and rough plan

The first step is a single-room end-to-end prototype: one set of sensors, the
controller, an HVAC actuator, and BuildSim. This tests whether the control loop closes
and whether the model produces meaningful room-level responses.

After that, the project expands to more rooms and adds a data-driven forecasting
component. The controller is refined using simulation results, failure handling is
tested (sensor restarts, dropped messages, delayed actuator commands), and the final
system is evaluated against a baseline fixed-schedule controller. The main goal is to
show that comfort can be maintained while reducing unnecessary energy consumption.

## Conclusion

This project addresses the challenge of maintaining comfortable indoor conditions while
minimising energy waste in a smart building. The proposed system uses occupancy,
temperature, and humidity data to optimise HVAC operation in real time. The design is
feasible within the course timeframe, aligned with the project guide, and suitable for
implementation in the BuildSim simulator. The approach is intentionally simple at
first, with a clear path toward a more advanced and energy-aware control system as the
project develops.
