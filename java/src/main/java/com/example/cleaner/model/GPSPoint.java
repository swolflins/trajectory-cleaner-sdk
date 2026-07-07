package com.example.cleaner.model;

/**
 * GNSS 轨迹点数据模型
 * 一个不可变的轨迹点，包含经纬度、时间戳和可选的精度信息
 */
public final class GPSPoint {

    /** 设备ID */
    private final String deviceId;

    /** 纬度 (WGS84) */
    private final double latitude;

    /** 经度 (WGS84) */
    private final double longitude;

    /** 时间戳 (毫秒) */
    private final long timestamp;

    /** 定位精度 (米)，可选，小于0表示无此字段 */
    private final double accuracy;

    /** 速度 (m/s)，可选，小于0表示无此字段 */
    private final double speed;

    /** 航向角 (度，0-360)，可选，小于0表示无此字段 */
    private final double heading;

    private GPSPoint(Builder builder) {
        this.deviceId = builder.deviceId;
        this.latitude = builder.latitude;
        this.longitude = builder.longitude;
        this.timestamp = builder.timestamp;
        this.accuracy = builder.accuracy;
        this.speed = builder.speed;
        this.heading = builder.heading;
    }

    public String getDeviceId() { return deviceId; }
    public double getLatitude() { return latitude; }
    public double getLongitude() { return longitude; }
    public long getTimestamp() { return timestamp; }
    public double getAccuracy() { return accuracy; }
    public double getSpeed() { return speed; }
    public double getHeading() { return heading; }

    public boolean hasAccuracy() { return accuracy >= 0; }
    public boolean hasSpeed() { return speed >= 0; }

    /**
     * 计算与另一个点之间的大圆距离 (米)
     * 使用 Haversine 公式
     */
    public double distanceTo(GPSPoint other) {
        double lat1 = Math.toRadians(this.latitude);
        double lat2 = Math.toRadians(other.latitude);
        double dLat = Math.toRadians(other.latitude - this.latitude);
        double dLon = Math.toRadians(other.longitude - this.longitude);

        double a = Math.sin(dLat / 2) * Math.sin(dLat / 2)
                + Math.cos(lat1) * Math.cos(lat2)
                * Math.sin(dLon / 2) * Math.sin(dLon / 2);
        double c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));

        return 6371000.0 * c; // 地球半径 6371km
    }

    /**
     * 计算与另一个点之间的时间差 (秒)
     */
    public double timeDiffSeconds(GPSPoint other) {
        return Math.abs(this.timestamp - other.timestamp) / 1000.0;
    }

    /**
     * 计算与另一个点之间的瞬时速度 (m/s)
     */
    public double velocityTo(GPSPoint other) {
        double dt = timeDiffSeconds(other);
        if (dt == 0) return 0;
        return distanceTo(other) / dt;
    }

    /**
     * 构建一个与当前点相同位置、不同时间戳的新点（用于替代异常点）
     */
    public GPSPoint withTimestamp(long newTimestamp) {
        return new Builder(deviceId, latitude, longitude, newTimestamp).build();
    }

    @Override
    public String toString() {
        return String.format("GPSPoint{lat=%.6f, lon=%.6f, ts=%d, acc=%.1f}", latitude, longitude, timestamp, accuracy);
    }

    /**
     * Builder 模式构建 GPSPoint
     */
    public static class Builder {
        private String deviceId;
        private double latitude;
        private double longitude;
        private long timestamp;
        private double accuracy = -1;
        private double speed = -1;
        private double heading = -1;

        public Builder(String deviceId, double latitude, double longitude, long timestamp) {
            this.deviceId = deviceId;
            this.latitude = latitude;
            this.longitude = longitude;
            this.timestamp = timestamp;
        }

        public Builder accuracy(double accuracy) { this.accuracy = accuracy; return this; }
        public Builder speed(double speed) { this.speed = speed; return this; }
        public Builder heading(double heading) { this.heading = heading; return this; }

        public GPSPoint build() { return new GPSPoint(this); }
    }
}
