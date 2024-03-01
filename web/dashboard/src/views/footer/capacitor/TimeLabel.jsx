/*
Copyright 2023 The Capacitor Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Original version: https://github.com/gimlet-io/capacitor/blob/main/web/src/TimeLabel.jsx
*/

import React, { useState, useEffect } from 'react';
import { formatDistance } from "date-fns";

export function TimeLabel(props) {
  const { title, date } = props;
  const [label, setLabel] = useState(formatDistance(date, new Date()));

  useEffect(() => {
    setLabel(formatDistance(date, new Date()));
  }, [date]);

  useEffect(() => {
    const interval = setInterval(() => {
      setLabel(formatDistance(date, new Date()));
    }, 60 * 1000);

    return () => clearInterval(interval);
  }, [date]);

  return (
    <span title={title}>{label}</span>
  )
}
